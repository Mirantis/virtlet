#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

VIRTLET_SKIP_RSYNC="${VIRTLET_SKIP_RSYNC:-}"
VIRTLET_RSYNC_PORT="${VIRTLET_RSYNC_PORT:-18730}"

# Note that project_dir must not end with slash
project_dir="$(cd "$(dirname "${BASH_SOURCE}")/.." && pwd)"
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

function ensure_build_image {
    # can't use 'docker images -q' due to https://github.com/docker/docker/issues/28895
    if ! docker history -q "${build_image}" >& /dev/null; then
        docker build -t "${build_image}" -f "${project_dir}/Dockerfile.build" "${project_dir}"
    fi
}

function get_rsync_addr {
    # from build/common.sh in k8s
    local mapped_port
    if ! mapped_port=$(docker port virtlet-build 8730 2> /dev/null | cut -d: -f 2) ; then
        echo "Could not get effective rsync port" >&2
        return 1
    fi

    local container_ip
    container_ip=$(docker inspect --format '{{ .NetworkSettings.IPAddress }}' virtlet-build)

    # Sometimes we can reach rsync through localhost and a NAT'd port.  Other
    # times (when we are running in another docker container on the Jenkins
    # machines) we have to talk directly to the container IP.  There is no one
    # strategy that works in all cases so we test to figure out which situation we
    # are in.
    if rsync_probe 127.0.0.1 ${mapped_port}; then
        echo "127.0.0.1:${mapped_port}" >"${project_dir}/_output/rsync_addr"
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
        # CRI proxy deployment tests
        # We also pass --tmpfs /tmp because log tailing doesn't work
        # on overlayfs. This breaks 'go test' though unless we also
        # remount /tmp with exec option (it creates and runs executable files
        # under /tmp)
        docker run -d --privileged \
               -l virtlet_build \
               -v "virtlet_src:${remote_project_dir}" \
               -v "virtlet_pkg:/go/pkg" \
               -v /sys/fs/cgroup:/sys/fs/cgroup \
               -v /lib/modules:/lib/modules:ro \
               -v /boot:/boot:ro \
               -v /var/run/docker.sock:/var/run/docker.sock \
               -e TRAVIS="${TRAVIS:-}" \
               -e CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT="${CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT:-}" \
               -p "${VIRTLET_RSYNC_PORT}:8730" \
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
            docker exec -d -i virtlet-build /rsyncd.sh
            get_rsync_addr
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
    docker exec virtlet-build bash -c "${cmd}"
}

function stop {
    docker ps -q --filter=label=virtlet_build | while read container_id; do
        echo >&2 "Removing container:" "${container_id}"
        docker rm -fv "${container_id}"
    done
}

function copy_output {
    ensure_build_container
    cd "${project_dir}"
    vcmd_simple "tar -C '${remote_project_dir}' -cz \$(find . -path '*/_output/*' -type f)" | tar -xvz
}

function copy_dind {
    if ! docker volume ls -q | grep -q '^kubeadm-dind-kube-node-1$'; then
        echo "No active or snapshotted kubeadm-dind-cluster" >&2
        exit 1
    fi
    ensure_build_image
    cd "${project_dir}"
    docker run --rm \
           -v "virtlet_src:${remote_project_dir}" \
           -v kubeadm-dind-kube-node-1:/dind \
           --name ${tmp_container_name} \
           "${build_image}" \
           /bin/sh -c "cp -av _output/* /dind"
}

function kvm_ok {
    # The check is done inside node-1 container because it has proper /lib/modules
    # from the docker host. Also, it'll have to use mirantis/virtlet image
    # later anyway.
    if ! docker exec kube-node-1 docker run --privileged --rm -v /lib/modules:/lib/modules mirantis/virtlet kvm-ok; then
        return 1
    fi
}

function start_dind {
    kubectl label node kube-node-1 extraRuntime=virtlet
    if kvm_ok; then
        kubectl convert -f "${project_dir}/deploy/virtlet-ds-dev.yaml" --local -o json |
            docker exec -i kube-master jq '.items[0].spec.template.spec.containers[0].env|=map(select(.name!="VIRTLET_DISABLE_KVM"))' |
            kubectl create -f -
    else
        kubectl create -f "${project_dir}/deploy/virtlet-ds-dev.yaml"
    fi
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
    start_libvirt=""
    # FIXME: exit 1 in $(virtlet_subdir) doesn't cause the script to exit
    virtlet_subdir >/dev/null
    subdir="$(virtlet_subdir)"
    if ! vcmd "${start_libvirt}cd '${subdir}' && go test $*"; then
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
    build)
        ( vcmd "./autogen.sh && ./configure && make" )
        ;;
    test)
        ( vcmd 'VIRTLET_DISABLE_KVM=y build/do-test.sh' )
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
    copy-dind)
        copy_dind
        ;;
    start-dind)
        start_dind
        ;;
    *)
        usage
        ;;
esac
