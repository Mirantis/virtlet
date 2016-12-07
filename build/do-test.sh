#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace
/start.sh -novirtlet

./autogen.sh
./configure
make
make install
if ! VIRTLET_DISABLE_KVM=1 make check; then
    find . -name test-suite.log | xargs cat
    exit 1
fi
