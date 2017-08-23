#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace
VIRTLET_DISABLE_KVM=1 /libvirt.sh -novirtlet

./autogen.sh
./configure
make
make install

if ! VIRTLET_DISABLE_KVM=1 make check; then
    find . -name test-suite.log | xargs cat >&2

    echo >&2 "***** libvirtd.log *****"
    cat /var/log/libvirt/libvirtd.log >&2

    exit 1
fi
