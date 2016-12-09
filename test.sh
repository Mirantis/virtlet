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

function dcompose {
    docker-compose -f contrib/docker-compose-test/docker-compose.yml "$@"
}

build/cmd.sh build
build/cmd.sh copy
build/cmd.sh test

dcompose down -v
dcompose build
dcompose run e2e_test
dcompose down -v
