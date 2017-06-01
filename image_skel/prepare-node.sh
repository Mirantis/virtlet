#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

PLUGIN_DIR=/kubelet-volume-plugins/virtlet~flexvolume_driver
BOOTSTRAP_LOG=/hostlog/criproxy-bootstrap.log
CRIPROXY_DEST=/opt/criproxy/bin/criproxy

if [[ ! -d ${PLUGIN_DIR} ]]; then
  mkdir "${PLUGIN_DIR}"
  if [[ -f /dind/flexvolume_driver ]]; then
    cp /dind/flexvolume_driver "${PLUGIN_DIR}/flexvolume_driver"
  else
    cp /flexvolume_driver "${PLUGIN_DIR}/flexvolume_driver"
  fi
fi

if [[ ! -f /etc/criproxy/kubelet.conf ]]; then
  if [[ ! -f /opt/criproxy/bin/criproxy ]]; then
    mkdir -p /opt/criproxy/bin
    if [[ -f /dind/criproxy ]]; then
      cp /dind/criproxy "${CRIPROXY_DEST}"
    else
      cp /criproxy "${CRIPROXY_DEST}"
    fi
  fi
  "${CRIPROXY_DEST}" -alsologtostderr -v 20 -install >> "${BOOTSTRAP_LOG}" 2>&1
fi

# Ensure that /var/lib/libvirt/images exists on node
mkdir -p /host-var-lib/libvirt/images

# Ensure that /var/log/virtlet/vms exists on node
mkdir -p /hostlog/virtlet/vms
