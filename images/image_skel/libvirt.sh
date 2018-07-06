#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [[ -f /var/lib/virtlet/config.sh ]]; then
  . /var/lib/virtlet/config.sh
fi

testmode=
if [[ ${1:-} == -testmode ]]; then
  testmode=1
fi

if [[ -f /dind/vmwrapper ]]; then
  ln -fs /dind/vmwrapper /vmwrapper
fi

function regenerate_qemu_conf() {
  if ls /sys/class/net/*/device/iommu_group >/dev/null 2>&1 ; then
    set $(ls -l /sys/class/net/*/device/iommu_group | sed 's@.*/\(.*\)@"/dev/vfio/\1",@')
    sed -i "s|# @DEVS@|$*|" /etc/libvirt/qemu.conf
  else
    echo WARNING - Virtlet is configured to use SR-IOV but no such resources are available on this host
    sed -i "/# @DEVS@/d" /etc/libvirt/qemu.conf
  fi
}

VIRTLET_SRIOV_SUPPORT="${VIRTLET_SRIOV_SUPPORT:-}"
if [[ ${VIRTLET_SRIOV_SUPPORT} ]] ; then
  regenerate_qemu_conf
else
  sed -i "/# @DEVS@/d" /etc/libvirt/qemu.conf
fi

chown root:root /etc/libvirt/libvirtd.conf
chown root:root /etc/libvirt/qemu.conf
chmod 644 /etc/libvirt/libvirtd.conf
chmod 644 /etc/libvirt/qemu.conf

# Without this hack qemu dies trying to unlink
# '/var/lib/libvirt/qemu/capabilities.monitor.sock'
# while libvirt is querying capabilities.
# Removal of the socket below helps but not always.

if [[ -e /var/lib/libvirt/qemu ]]; then
  mv /var/lib/libvirt/qemu /var/lib/libvirt/qemu.ok
  mv /var/lib/libvirt/qemu.ok /var/lib/libvirt/qemu
fi

# export LIBVIRT_LOG_FILTERS="1:qemu.qemu_process 1:qemu.qemu_command 1:qemu.qemu_domain"
# export LIBVIRT_DEBUG=1

# only make vmwrapper suid in libvirt container
chown root.root /vmwrapper
chmod ug+s /vmwrapper

if [[ ${testmode} ]]; then
  # leftover socket prevents libvirt from initializing correctly
  rm -f /var/lib/libvirt/qemu/capabilities.monitor.sock
  /usr/local/sbin/libvirtd --listen --daemon
else
  # FIXME: try using exec liveness probe instead
  while true; do
    /usr/local/sbin/libvirtd --listen
    sleep 1
  done
fi
