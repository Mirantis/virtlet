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

if [[ ! -f /etc/criproxy/node.conf ]]; then
  if [[ ! -f /opt/criproxy/bin/criproxy ]]; then
    mkdir -p /opt/criproxy/bin
    if [[ -f /dind/criproxy ]]; then
      cp /dind/criproxy "${CRIPROXY_DEST}"
    else
      cp /criproxy "${CRIPROXY_DEST}"
    fi
  fi
  # we need to be in host mount/uts namespace to do the grabbing
  # (uts namespace is needed to get the node name), but let's
  # just use all the namespaces to be on the safe side
  nsenter -t 1 -m -u -i -n "${CRIPROXY_DEST}" -alsologtostderr -v 20 -grab >> "${BOOTSTRAP_LOG}" 2>&1
  if ! "${CRIPROXY_DEST}" -alsologtostderr -v 20 -install >> "${BOOTSTRAP_LOG}" 2>&1; then
    rm -rf /etc/criproxy
    exit 1
  fi
fi

# Ensure that the dirs required by virtlet exist on the node
mkdir -p /host-var-lib/libvirt/images /hostlog/virtlet/vms /host-var-lib/virtlet/volumes
