#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [[ /var/lib/virtlet/config.sh ]]; then
  . /var/lib/virtlet/config.sh
fi

if [[ -f /dind/virtlet ]]; then
  ln -fs /dind/virtlet /usr/local/bin/virtlet
fi

while [ ! -S /var/run/libvirt/libvirt-sock ] ; do
  echo >&1 "Waiting for libvirt..."
  sleep 0.3
done

verbose=
if [[ ${VIRTLET_LOGLEVEL:-} ]]; then
    verbose="--v ${VIRTLET_LOGLEVEL}"
fi
/usr/local/bin/virtlet ${verbose}
