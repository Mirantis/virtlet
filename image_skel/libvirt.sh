#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

novirtlet=
if [[ ${1:-} == -novirtlet ]]; then
  novirtlet=1
fi

if [[ -f /dind/vmwrapper ]]; then
  ln -fs /dind/vmwrapper /vmwrapper
fi

if [[ ! ${VIRTLET_DISABLE_KVM:-} ]]; then
  if ! kvm-ok >&/dev/null; then
    # try to fix the environment by loading appropriate modules
    modprobe kvm || ((echo "Missing kvm module on the host" >&2 && exit 1))
    if grep vmx /proc/cpuinfo &>/dev/null; then
      modprobe kvm_intel || (echo "Missing kvm_intel module on the host" >&2 && exit 1)
    elif grep svm /proc/cpuinfo &>/dev/null; then
      modprobe kvm_amd || (echo "Missing kvm_amd module on the host" >&2 && exit 1)
    fi
  fi
  if ! kvm-ok; then
    echo "*** VIRTLET_DISABLE_KVM is not set but KVM extensions are not available ***" >&2
    echo "*** Virtlet startup failed ***" >&2
    exit 1
  fi
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

daemon=
if [[ ${novirtlet} ]]; then
  # leftover socket prevents libvirt from initializing correctly
  rm -f /var/lib/libvirt/qemu/capabilities.monitor.sock
  daemon="--daemon"  
fi

if [[ ! ${VIRTLET_DISABLE_KVM:-} ]]; then
  chown root:kvm /dev/kvm
fi

/usr/sbin/libvirtd --listen $daemon
