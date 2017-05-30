#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

# Note that project_dir must not end with slash
project_dir="$(cd "$(dirname "${BASH_SOURCE}")/.." && pwd)"
remote_project_dir="/go/src/github.com/Mirantis/virtlet"
build_name="virtlet_build"
container_name="${build_name}-$(openssl rand -hex 16)"
build_image=${build_name}:latest
volume_name=virtlet_src
skip_image_check=
exclude=(
    --exclude 'vendor'
    --exclude .git
    --exclude _output
    --exclude '*.png'
)

function ensure_build_image {
    # for commands like 'gotest' and 'gobuild' which are usually
    # invoked by an editor with possibility that their output may be
    # delayed, there's no sense in building the image here or wasting
    # time checking for it
    if [[ ${skip_image_check} ]]; then
        return
    fi
    # can't use 'docker images -q' due to https://github.com/docker/docker/issues/28895
    if ! docker history -q "${build_image}" >& /dev/null; then
        docker build -t "${build_image}" -f "${project_dir}/Dockerfile.build" "${project_dir}"
    fi
}

function vsh {
    ensure_build_image
    cd "${project_dir}"
    docker run --rm --privileged -it \
           -l virtlet_build \
           -v "virtlet_src:${remote_project_dir}" \
           -v "virtlet_pkg:/go/pkg" \
           -v /sys/fs/cgroup:/sys/fs/cgroup \
           -v /lib/modules:/lib/modules:ro \
           -v /boot:/boot:ro \
           -v /var/run/docker.sock:/var/run/docker.sock \
           -e TRAVIS="${TRAVIS:-}" \
           -e CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT="${CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT:-}" \
           --name ${container_name} \
           "${build_image}" env TERM=xterm bash
}

function vcmd {
    ensure_build_image
    cd "${project_dir}"
    # need to mount docker socket into the container because of
    # CRI proxy deployment tests
    tar -C "${project_dir}" "${exclude[@]}" -cz . |
        docker run --rm --privileged -i \
               -l virtlet_build \
               -v "virtlet_src:${remote_project_dir}" \
               -v "virtlet_pkg:/go/pkg" \
               -v /sys/fs/cgroup:/sys/fs/cgroup \
               -v /lib/modules:/lib/modules:ro \
               -v /boot:/boot:ro \
               -v /var/run/docker.sock:/var/run/docker.sock \
               -e TRAVIS="${TRAVIS:-}" \
               -e CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT="${CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT:-}" \
               --name ${container_name} \
               "${build_image}" bash -c "tar -C '${remote_project_dir}' -xz && $*"
}

function stop {
  docker ps -q --filter=label=virtlet_build | while read container_id; do
    echo >&2 "Removing container:" "${container_id}"
    docker rm -fv "${container_id}"
  done
}

function copy_output {
    ensure_build_image
    cd "${project_dir}"
    docker run --rm --privileged -i \
           -v "virtlet_src:${remote_project_dir}" \
           -v "virtlet_pkg:/go/pkg" \
           --name ${container_name} \
           "${build_image}" \
           bash -c "tar -C '${remote_project_dir}' -cz \$(find . -path '*/_output/*' -type f)" |
        tar -xvz
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
           --name ${container_name} \
           "${build_image}" \
           /bin/sh -c "cp -av _output/* /dind"
}

function start_dind {
  kubectl label node kube-node-1 extraRuntime=virtlet
  kubectl create -f "${project_dir}/deploy/virtlet-ds-dev.yaml"
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
    subdir="$(virtlet_subdir)"
    if [[ ${subdir} =~ /integration$ ]]; then
      start_libvirt="VIRTLET_DISABLE_KVM=${VIRTLET_DISABLE_KVM:-} /start.sh -novirtlet && "
    fi
    vcmd "${start_libvirt}cd '${subdir}' && go test $*"
}

function gobuild {
    vcmd "cd '$(virtlet_subdir)' && go build $*"
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
        skip_image_check=y
        gotest "$@"
        ;;
    gobuild)
        skip_image_check=y
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
