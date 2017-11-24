#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

CRIPROXY_DEB_URL="${CRIPROXY_DEB_URL:-https://github.com/Mirantis/criproxy/releases/download/v0.9.2/criproxy-nodeps_0.9.2_amd64.deb}"
VIRTLET_IMAGE="${VIRTLET_IMAGE:-mirantis/virtlet}"
VIRTLET_SKIP_RSYNC="${VIRTLET_SKIP_RSYNC:-}"
VIRTLET_RSYNC_PORT="${VIRTLET_RSYNC_PORT:-18730}"
VIRTLET_ON_MASTER="${VIRTLET_ON_MASTER:-}"
# XXX: try to extract the docker socket path from DOCKER_HOST if it's set to unix://...
DOCKER_SOCKET_PATH="${DOCKER_SOCKET_PATH:-/var/run/docker.sock}"
FORCE_UPDATE_IMAGE="${FORCE_UPDATE_IMAGE:-}"
IMAGE_REGEXP_TRANSLATION="${IMAGE_REGEXP_TRANSLATION:-1}"

# Note that project_dir must not end with slash
project_dir="$(cd "$(dirname "${BASH_SOURCE}")/.." && pwd)"
virtlet_image="mirantis/virtlet"
remote_project_dir="/go/src/github.com/Mirantis/virtlet"
build_name="virtlet_build"
tmp_container_name="${build_name}-$(openssl rand -hex 16)"
build_image=${build_name}:latest
volume_name=virtlet_src
rsync_git=y
exclude=(
    --exclude 'vendor'
    --exclude .git
    --exclude _output
    --exclude '*.png'
)
rsync_pw_file="${project_dir}/_output/rsync.password"
busybox_image=busybox:1.27.2
virtlet_node=kube-node-1
if [[ ${VIRTLET_ON_MASTER} ]]; then
  virtlet_node=kube-master
fi

dind_mounts='.items[0].spec.template.spec.volumes|=.+[{"name":"dind",hostPath:{"path":"/dind"}}]|.items[0].spec.template.spec.containers[].volumeMounts|=.+[{"name":"dind", "mountPath":"/dind"}]|.items[0].spec.template.spec.initContainers[].volumeMounts|=.+[{"name":"dind", "mountPath":"/dind"}]'

# from build/common.sh in k8s
function rsync_probe {
    # Wait unil rsync is up and running.
    local tries=20
    while (( ${tries} > 0 )) ; do
        if rsync "rsync://k8s@${1}:${2}/" \
                 --password-file="${project_dir}/_output/rsyncd.password" \
           &> /dev/null ; then
            return 0
        fi
        tries=$(( ${tries} - 1))
        sleep 0.1
    done

    return 1
}

function image_exists {
    local name="${1}"
    # can't use 'docker images -q' due to https://github.com/docker/docker/issues/28895
    docker history -q "${name}" >& /dev/null || return 1
}

function update_dockerfile_from {
    local dockerfile="${1}"
    local from_dockerfile="${2}"
    local dest_var="${3:-}"
    local cur_from="$(awk '/^FROM /{print $2}' "${dockerfile}")"
    if [[ ${cur_from} =~ (^.*:.*-)([0-9a-f]) ]]; then
        new_from="${BASH_REMATCH[1]}$(md5sum ${from_dockerfile} | sed 's/ .*//')"
        if [[ ${new_from} != ${cur_from} ]]; then
            sed -i "s@^FROM .*@FROM ${new_from}@" "${dockerfile}"
        fi
        if [[ ${dest_var} ]]; then
            eval "${dest_var}=${new_from}"
        fi
    else
        echo >&2 "*** ERROR: can't update FROM in ${dockerfile}: unexpected value: '${cur_from}'"
        return 1
    fi
}

function ensure_build_image {
    update_dockerfile_from "${project_dir}/images/Dockerfile.build-base" "${project_dir}/images/Dockerfile.virtlet-base" virtlet_base_image
    update_dockerfile_from "${project_dir}/images/Dockerfile.build" "${project_dir}/images/Dockerfile.build-base" build_base_image
    update_dockerfile_from "${project_dir}/images/Dockerfile.virtlet" "${project_dir}/images/Dockerfile.virtlet-base"

    if ! image_exists "${build_image}"; then
        if ! image_exists "${build_base_image}"; then
            if ! image_exists "${virtlet_base_image}"; then
                echo >&2 "Trying to pull the base image ${virtlet_base_image}..."
                if ! docker pull "${virtlet_base_image}"; then
                    docker build -t "${virtlet_base_image}" -f "${project_dir}/images/Dockerfile.virtlet-base" "${project_dir}/images"
                fi
            fi
            echo >&2 "Trying to pull the build base image ${build_base_image}..."
            if ! docker pull "${build_base_image}"; then
                docker build -t "${build_base_image}" \
                       --label virtlet_image=build-base \
                       -f "${project_dir}/images/Dockerfile.build-base" "${project_dir}/images"
            fi
        fi
        tar -C "${project_dir}/images" -c image_skel/ qemu-build.conf Dockerfile.build |
            docker build -t "${build_image}" -f Dockerfile.build -
    fi
}

function get_rsync_addr {
    local container_ip
    container_ip=$(docker inspect --format '{{ .NetworkSettings.IPAddress }}' virtlet-build)

    # Sometimes we can reach rsync through localhost and a NAT'd port.  Other
    # times (when we are running in another docker container on the Jenkins
    # machines) we have to talk directly to the container IP.  There is no one
    # strategy that works in all cases so we test to figure out which situation we
    # are in.
    if rsync_probe 127.0.0.1 "${VIRTLET_RSYNC_PORT}"; then
        echo "127.0.0.1:${VIRTLET_RSYNC_PORT}" >"${project_dir}/_output/rsync_addr"
        return 0
    elif rsync_probe "${container_ip}" ${VIRTLET_RSYNC_PORT}; then
        echo "${container_ip}:${VIRTLET_RSYNC_PORT}" >"${project_dir}/_output/rsync_addr"
        return 0
    else
        echo "Could not probe the rsync port" >&2
    fi
}

function ensure_build_container {
    if ! docker ps --filter=label=virtlet_build | grep -q virtlet-build; then
        ensure_build_image
        cd "${project_dir}"
        # Need to mount docker socket into the container because of
        # CRI proxy deployment tests & building the image
        # We also pass --tmpfs /tmp because log tailing doesn't work
        # on overlayfs. This breaks 'go test' though unless we also
        # remount /tmp with exec option (it creates and runs executable files
        # under /tmp)
        declare -a docker_cert_args=()
        if [[ ${DOCKER_CERT_PATH:-} ]]; then
           docker_cert_args=(-e DOCKER_CERT_PATH=/docker-cert)
        fi
        docker run -d --privileged --net=host \
               -l virtlet_build \
               -v "virtlet_src:${remote_project_dir}" \
               -v "virtlet_pkg:/go/pkg" \
               -v /sys/fs/cgroup:/sys/fs/cgroup \
               -v /lib/modules:/lib/modules:ro \
               -v /boot:/boot:ro \
               -v "${DOCKER_SOCKET_PATH}:/var/run/docker.sock" \
               -e DOCKER_HOST="${DOCKER_HOST:-}" \
               -e DOCKER_MACHINE_NAME="${DOCKER_MACHINE_NAME:-}" \
               -e DOCKER_TLS_VERIFY="${DOCKER_TLS_VERIFY:-}" \
               -e TRAVIS="${TRAVIS:-}" \
               -e TRAVIS_PULL_REQUEST="${TRAVIS_PULL_REQUEST:-}" \
               -e TRAVIS_BRANCH="${TRAVIS_BRANCH:-}" \
               -e CIRCLECI="${CIRCLECI:-}" \
               -e CIRCLE_PULL_REQUEST="${CIRCLE_PULL_REQUEST:-}" \
               -e CIRCLE_BRANCH="${CIRCLE_PULL_REQUEST:-}" \
               -e VIRTLET_ON_MASTER="${VIRTLET_ON_MASTER:-}" \
               ${docker_cert_args[@]+"${docker_cert_args[@]}"} \
               --name virtlet-build \
               --tmpfs /tmp \
               "${build_image}" \
               /bin/bash -c "mount /tmp -o remount,exec && sleep Infinity" >/dev/null
        if [[ ! ${VIRTLET_SKIP_RSYNC} ]]; then
            # from build/common.sh in k8s
            mkdir -p "${project_dir}/_output"
            dd if=/dev/urandom bs=512 count=1 2>/dev/null | LC_ALL=C tr -dc 'A-Za-z0-9' | dd bs=32 count=1 2>/dev/null >"${rsync_pw_file}"
            chmod 600 "${rsync_pw_file}"

            docker cp "${rsync_pw_file}" virtlet-build:/rsyncd.password
            docker exec -d -i virtlet-build /rsyncd.sh "${VIRTLET_RSYNC_PORT}"
            get_rsync_addr
        fi
        if [[ ${DOCKER_CERT_PATH:-} ]]; then
            tar -C "${DOCKER_CERT_PATH}" -c . | docker exec -i virtlet-build /bin/bash -c 'mkdir /docker-cert && tar -C /docker-cert -xv'
        fi
    fi
    if [[ ! ${VIRTLET_SKIP_RSYNC} ]]; then
        RSYNC_ADDR="$(cat "${project_dir}/_output/rsync_addr")"
    fi
}

function vsh {
    ensure_build_container
    cd "${project_dir}"
    docker exec -it virtlet-build env TERM=xterm bash
}

function vcmd {
    ensure_build_container
    cd "${project_dir}"
    if [[ ! ${VIRTLET_SKIP_RSYNC} ]]; then
        local -a filters=(
            --filter '- /vendor/'
            --filter '- /_output/'
        )
        if [[ ! ${rsync_git} ]]; then
            filters+=(--filter '- /.git/')
        fi
        rsync "${filters[@]}" \
              --password-file "${project_dir}/_output/rsync.password" \
              -a --delete --compress-level=9 \
              "${project_dir}/" "rsync://virtlet@${RSYNC_ADDR}/virtlet/"
    fi
    docker exec -i virtlet-build bash -c "$*"
}

function vcmd_simple {
    local cmd="${1}"
    docker exec -i virtlet-build bash -c "${cmd}"
}

function stop {
    docker ps -a -q --filter=label=virtlet_build | while read container_id; do
        echo >&2 "Removing container:" "${container_id}"
        docker rm -fv "${container_id}"
    done
}

function copy_output {
    ensure_build_container
    cd "${project_dir}"
    vcmd_simple "tar -C '${remote_project_dir}' -cz \$(find . -path '*/_output/*' -type f)" | tar -xvz
}

function copy_back {
    ensure_build_container
    cd "${project_dir}"
    tar -cz $(find . -path '*/_output/*' -type f | grep -v rsync) | vcmd_simple "tar -C '${remote_project_dir}' -xvz"
}

function copy_dind_internal {
    if ! docker volume ls -q | grep -q "^kubeadm-dind-${virtlet_node}$"; then
        echo "No active or snapshotted kubeadm-dind-cluster" >&2
        exit 1
    fi
    tar -C _output -c . |
        docker run -i --rm \
               -v "kubeadm-dind-${virtlet_node}:/dind" \
               --name ${tmp_container_name} \
               "${busybox_image}" \
               /bin/sh -c 'tar -C /dind -xv && chmod ug+s /dind/vmwrapper'
}

function kvm_ok {
    # The check is done inside the virtlet node container because it
    # has proper /lib/modules from the docker host. Also, it'll have
    # to use the virtlet image later anyway.
    if ! docker exec "${virtlet_node}" docker run --privileged --rm -v /lib/modules:/lib/modules "${VIRTLET_IMAGE}" kvm-ok; then
        return 1
    fi
}

function start_dind {
    if ! docker exec "${virtlet_node}" dpkg-query -W criproxy-nodeps >&/dev/null; then
        echo >&2 "Installing CRI proxy package the node container..."
        docker exec "${virtlet_node}" /bin/bash -c "curl -sSL '${CRIPROXY_DEB_URL}' >/criproxy.deb && dpkg -i /criproxy.deb && rm /criproxy.deb"
    fi
    if [[ ${VIRTLET_ON_MASTER} ]]; then
        if [[ $(kubectl get node kube-master -o jsonpath='{.spec.taints[?(@.key=="node-role.kubernetes.io/master")]}') ]]; then
            kubectl taint nodes kube-master node-role.kubernetes.io/master-
        fi
    fi
    if [[ ${FORCE_UPDATE_IMAGE} ]] || ! docker exec "${virtlet_node}" docker history -q mirantis/virtlet:latest >&/dev/null; then
        echo >&2 "Propagating Virtlet image to the node container..."
        vcmd "docker save '${virtlet_image}' | docker exec -i '${virtlet_node}' docker load"
    fi
    local -a virtlet_config=(--from-literal=image_regexp_translation="${IMAGE_REGEXP_TRANSLATION}")
    if ! kvm_ok; then
        virtlet_config+=(--from-literal=disable_kvm=y)
    fi
    kubectl label node --overwrite "${virtlet_node}" extraRuntime=virtlet
    kubectl create configmap -n kube-system virtlet-config "${virtlet_config[@]}"
    kubectl create configmap -n kube-system virtlet-image-translations --from-file "${project_dir}/deploy/images.yaml"
    kubectl convert --local -o json -f "${project_dir}/deploy/virtlet-ds.yaml" |
       jq "${dind_mounts}" |
       kubectl apply -f -
}

function virtlet_subdir {
    local dir="${1:-$(pwd)}"
    local prefix="${project_dir}/"
    if [[ ${#dir} -lt ${#prefix} || ${dir:0:${#prefix}} != ${prefix} ]]; then
        echo >&2 "must be in a project subdir"
        exit 1
    fi
    echo -n "${dir:${#prefix}}"
}

function clean {
    stop
    docker volume rm virtlet_src || true
    docker volume rm virtlet_pkg || true
    docker rmi "${build_image}" || true
    # find command may produce zero results
    # -exec rm -rf '{}' ';' produces errors when trying to
    # enter deleted directories
    find . -name _output -type d | while read dir; do
        rm -rf "${dir}"
    done
}

function gotest {
    # FIXME: exit 1 in $(virtlet_subdir) doesn't cause the script to exit
    virtlet_subdir >/dev/null
    subdir="$(virtlet_subdir)"
    if ! vcmd "cd '${subdir}' && go test $*"; then
        vcmd_simple "find . -name 'Test*.json' | xargs tar -c -T -" | tar -C "${project_dir}" -x
        exit 1
    fi
}

function gobuild {
    # FIXME: exit 1 in $(virtlet_subdir) doesn't cause the script to exit
    virtlet_subdir >/dev/null
    # -gcflags -e removes the limit on error message count, which helps
    # with using it for syntax checking
    vcmd "cd '$(virtlet_subdir)' && go build -gcflags -e $*"
}

function build_image_internal {
    build_internal
    tar -c _output -C "${project_dir}/images" image_skel/ Dockerfile.virtlet |
        docker build -t "${virtlet_image}" -f Dockerfile.virtlet -
}

function install_vendor_internal {
    if ! hash glide 2>/dev/null; then
        go get github.com/Masterminds/glide
    fi
    if [ ! -d vendor ]; then
        glide install --strip-vendor
    fi
}

function run_tests_internal {
    install_vendor_internal
    go test -v ./pkg/... ./tests/network/...
}

function run_integration_internal {
    install_vendor_internal
    ( cd tests/integration && ./go.test )
}

function build_internal {
    install_vendor_internal
    mkdir -p "${project_dir}/_output"
    go build -i -o "${project_dir}/_output/virtlet" ./cmd/virtlet
    go build -i -o "${project_dir}/_output/vmwrapper" ./cmd/vmwrapper
    go build -i -o "${project_dir}/_output/flexvolume_driver" ./cmd/flexvolume_driver
    go test -i -c -o "${project_dir}/_output/virtlet-e2e-tests" ./tests/e2e
}

function e2e {
    ensure_build_container
    local cluster_url
    cluster_url="$(kubectl config view -o jsonpath='{.clusters[?(@.name=="dind")].cluster.server}')"
    docker exec virtlet-build _output/virtlet-e2e-tests -cluster-url "${cluster_url}" "$@"
}

function usage {
    echo >&2 "Usage:"
    echo >&2 "  $0 build"
    echo >&2 "  $0 test"
    echo >&2 "  $0 copy"
    echo >&2 "  $0 copy-dind"
    echo >&2 "  $0 start-dind"
    echo >&2 "  $0 vsh"
    echo >&2 "  $0 stop"
    echo >&2 "  $0 clean"
    echo >&2 "  $0 gotest [TEST_ARGS...]"
    echo >&2 "  $0 gobuild [BUILD_ARGS...]"
    echo >&2 "  $0 run CMD..."
    exit 1
}

cmd="${1:-}"
if [[ ! $cmd ]]; then
    usage
fi
shift

case "${cmd}" in
    gotest)
        gotest "$@"
        ;;
    gobuild)
        rsync_git=
        gobuild "$@"
        ;;
    prepare-vendor)
        ( vcmd "build/cmd.sh install-vendor-internal" )
        ;;
    build)
        ( vcmd "build/cmd.sh build-image-internal" )
        ;;
    build-image-internal)
        # this is executed inside the container
        build_image_internal "$@"
        ;;
    test)
        ( vcmd 'build/cmd.sh run-tests-internal' )
        ;;
    integration)
        ( vcmd 'build/cmd.sh run-integration-internal' )
        ;;
    install-vendor-internal)
        install_vendor_internal
        ;;
    run-tests-internal)
        run_tests_internal
        ;;
    run-integration-internal)
        run_integration_internal
        ;;
    run)
        vcmd "$*"
        ;;
    vsh)
        vsh
        ;;
    stop)
        stop
        ;;
    clean)
        clean
        ;;
    copy)
        copy_output
        ;;
    copy-back)
        copy_back
        ;;
    copy-dind)
        VIRTLET_SKIP_RSYNC=y vcmd "build/cmd.sh copy-dind-internal"
        ;;
    e2e)
        e2e "$@"
        ;;
    copy-dind-internal)
        copy_dind_internal
        ;;
    start-dind)
        start_dind
        ;;
    start-build-container)
        ensure_build_container
        ;;
    *)
        usage
        ;;
esac

# TODO: make it possible to run e2e from within the build container, too
# (although we don't need to use that for CircleCI)
# TODO: fix indentation in this file (use 2 spaces)
