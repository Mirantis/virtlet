#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [ $(uname) = Darwin ]; then
  readlinkf(){ perl -MCwd -e 'print Cwd::abs_path shift' "$1";}
else
  readlinkf(){ readlink -f "$1"; }
fi

SCRIPT_DIR="$(cd $(dirname "$(readlinkf "${BASH_SOURCE}")"); pwd)"
cd "${SCRIPT_DIR}"

build/cmd.sh build
build/cmd.sh copy
if [[ ! ${SKIP_TEST:-} ]]; then
  build/cmd.sh test
fi

docker build -t mirantis/virtlet .

VIRTLET_DEMO_RELEASE=master NONINTERACTIVE=1 NO_VM_CONSOLE=1 INJECT_LOCAL_IMAGE=1 BASE_LOCATION="${SCRIPT_DIR}" deploy/demo.sh
tests/e2e/e2e.sh
