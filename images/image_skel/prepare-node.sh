#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

# make debugging this script easier
if [[ -f /dind/prepare-node.sh && ! ( ${0} =~ /dind/ ) ]]; then
    exec /dind/prepare-node.sh "$@"
fi

PLUGIN_DIR=/kubelet-volume-plugins/virtlet~flexvolume_driver

if [[ ! -d ${PLUGIN_DIR} ]]; then
  mkdir "${PLUGIN_DIR}"
  if [[ -f /dind/flexvolume_driver ]]; then
    cp /dind/flexvolume_driver "${PLUGIN_DIR}/flexvolume_driver"
  else
    cp /flexvolume_driver "${PLUGIN_DIR}/flexvolume_driver"
  fi
  # XXX: rm redir
  nsenter -t 1 -m -u -i -n /bin/sh -c "systemctl restart kubelet" >& /hostlog/xxx || true
fi

# Ensure that the dirs required by virtlet exist on the node
mkdir -p /host-var-lib/libvirt/images /hostlog/virtlet/vms /host-var-lib/virtlet/volumes
