#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

# make debugging this script easier
if [[ -f /dind/prepare-node.sh && ! ( ${0} =~ /dind/ ) ]]; then
    exec /dind/prepare-node.sh "$@"
fi

verbose=
if [[ ${VIRTLET_LOGLEVEL:-} ]]; then
    verbose="--v ${VIRTLET_LOGLEVEL}"
fi
/usr/local/bin/virtlet --dump-config ${verbose} >/var/lib/virtlet/config.sh
. /var/lib/virtlet/config.sh

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
mkdir -p /host-var-lib/virtlet/images /hostlog/virtlet/vms /host-var-lib/virtlet/volumes

# set up KVM
if [[ ! ${VIRTLET_DISABLE_KVM:-} ]]; then
  if ! kvm-ok &>/dev/null; then
    # try to fix the environment by loading appropriate modules
    modprobe kvm || (echo "Missing kvm module on the host" >&2 && sleep infinity)
    if grep vmx /proc/cpuinfo &>/dev/null; then
      modprobe kvm_intel || (echo "Missing kvm_intel module on the host" >&2 && sleep infinity)
    elif grep svm /proc/cpuinfo &>/dev/null; then
      modprobe kvm_amd || (echo "Missing kvm_amd module on the host" >&2 && sleep infinity)
    fi
  fi
  if [[ ! -e /dev/kvm ]] && ! mknod /dev/kvm c 10 $(grep '\<kvm\>' /proc/misc | cut -d" " -f1); then
    echo "Can't create /dev/kvm" >&2
  fi
  while ! kvm-ok; do
    echo "*** VIRTLET_DISABLE_KVM is not set but KVM extensions are not available ***" >&2
    echo "*** Virtlet startup failed ***" >&2
    echo "Rechecking in 5 sec." >&2
    sleep 5
  done
  chown libvirt-qemu.kvm /dev/kvm
fi
