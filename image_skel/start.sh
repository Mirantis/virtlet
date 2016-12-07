#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [[ ! ${VIRTLET_DISABLE_KVM:-} ]]; then
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

if [[ ${LIBVIRT_CLEANUP:-} ]]; then
	/usr/sbin/libvirtd -d
	/cleanup.py
	kill -9 $(cat /var/run/libvirtd.pid)
fi

if [[ ! ${VIRTLET_DISABLE_KVM:-} ]]; then
    chown root:kvm /dev/kvm
fi

/usr/sbin/libvirtd --listen -d

while ! nc -z -v -w1 localhost 16509 >& /dev/null; do
    echo >&1 "Waiting for libvirt..."
    sleep 0.3
done

if [[ ${1:-} != -novirtlet ]]; then
    /usr/local/bin/virtlet -v=${VIRTLET_LOGLEVEL:-2} -logtostderr=true -libvirt-uri=qemu+tcp://localhost/system
fi
