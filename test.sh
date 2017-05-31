#!/bin/bash -x
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

bash -x build/cmd.sh build
bash -x build/cmd.sh copy
# bash -x build/cmd.sh test

bash -x build/cmd.sh stop

echo "verifying VM state" >&2
docker ps
ps aux
df -h
free
echo "done verifying VM state" >&2

docker build -t mirantis/virtlet .

NONINTERACTIVE=1 NO_VM_CONSOLE=1 INJECT_LOCAL_IMAGE=1 BASE_LOCATION="${SCRIPT_DIR}" bash -x deploy/demo.sh
bash -x tests/e2e/e2e.sh
