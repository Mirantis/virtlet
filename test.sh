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

cd "$(dirname "$(readlinkf "${BASH_SOURCE}")")"

build/cmd.sh build
build/cmd.sh copy
build/cmd.sh test

docker build -t mirantis/virtlet .

NONINTERACTIVE=1 NO_VM_CONSOLE=1 INJECT_LOCAL_IMAGE=1 deploy/demo.sh
tests/e2e/e2e.sh
