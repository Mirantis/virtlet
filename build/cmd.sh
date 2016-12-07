#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

# Note that project_dir must not end with slash
project_dir="$(cd "$(dirname "${BASH_SOURCE}")/.." && pwd)"
remote_project_dir="/go/src/github.com/Mirantis/virtlet"
build_image=virtlet_build:latest
volume_name=virtlet_src
exclude=(
    --exclude 'vendor'
    --exclude .git
)

function ensure_build_image {
    # can't use 'docker images -q' due to https://github.com/docker/docker/issues/28895
    if ! docker history -q "${build_image}" >& /dev/null; then
        docker build -t "${build_image}" -f Dockerfile.build "${project_dir}"
    fi
}

function vcmd {
    ensure_build_image
    cd "${project_dir}"
    tar -C "${project_dir}" "${exclude[@]}" -cz . |
        docker run --rm --privileged -i \
               -v "virtlet_src:${remote_project_dir}" \
               -v "virtlet_pkg:/go/pkg" \
               -v /sys/fs/cgroup:/sys/fs/cgroup \
               -v /lib/modules:/lib/modules:ro \
               -v /boot:/boot:ro \
               "${build_image}" bash -c "tar -C '${remote_project_dir}' -xz && $*"
}

function copy_output {
    ensure_build_image
    cd "${project_dir}"
    docker run --rm --privileged -i \
           -v "virtlet_src:${remote_project_dir}" \
           -v "virtlet_pkg:/go/pkg" \
           "${build_image}" \
           bash -c "tar -C '${remote_project_dir}' -cz \$(find . -path '*/_output/*' -type f)" |
        tar -xvz
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
    docker volume rm -f virtlet_src || true
    docker volume rm -f virtlet_pkg || true
    docker rmi "${build_image}" || true
    # find command may produce zero results
    # -exec rm -rf '{}' ';' produces errors when trying to
    # enter deleted directories
    find . -name _output -type d | while read dir; do
        rm -rf "${dir}"
    done
}

function usage {
    echo >&2 "Usage:"
    echo >&2 "  $0 build"
    echo >&2 "  $0 test"
    echo >&2 "  $0 copy"
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
        ( vcmd "cd '$(virtlet_subdir)' && go test $*" )
        ;;
    gobuild)
        ( vcmd "cd '$(virtlet_subdir)' && go build $*" )
        ;;
    build)
        ( vcmd "./autogen.sh && ./configure && make" )
        ;;
    test)
        ( vcmd 'VIRTLET_DISABLE_KVM=1 build/do-test.sh' )
        ;;
    run)
        vcmd "$*"
        ;;
    clean)
        clean
        ;;
    copy)
        copy_output
        ;;
    *)
        usage
        ;;
esac
