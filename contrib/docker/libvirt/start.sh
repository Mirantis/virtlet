#!/bin/bash

set -o errexit

if [[ ! "${VIRTLET_DISABLE_KVM:-}" ]]; then
    modprobe kvm || ((echo "Missing kvm module on host" && exit 1))
    if grep vmx /proc/cpuinfo &>/dev/null; then
	modprobe kvm_intel || (echo "Missing kvm_intel module on host" && exit 1)
    elif grep svm /proc/cpuinfo &>/dev/null; then
	modprobe kvm_amd || (echo "Missing kvm_amd module on host" && exit 1)
    fi
fi

chown root:root /etc/libvirt/libvirtd.conf
chown root:root /etc/libvirt/qemu.conf
chmod 644 /etc/libvirt/libvirtd.conf
chmod 644 /etc/libvirt/qemu.conf

if [[ -n "$LIBVIRT_CLEANUP" ]]; then
	/usr/sbin/libvirtd -d
	/cleanup.py
	kill -9 $(cat /var/run/libvirtd.pid)
fi

if [[ ! "${VIRTLET_DISABLE_KVM:-}" ]]; then
    chown root:kvm /dev/kvm
fi
exec /usr/sbin/libvirtd --listen
