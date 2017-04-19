#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [[ -f /dind/virtlet ]]; then
  ln -fs /dind/virtlet /usr/local/bin/virtlet
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

# leftover socket prevents libvirt from initializing correctly
rm -f /var/lib/libvirt/qemu/capabilities.monitor.sock

if [[ ${LIBVIRT_CLEANUP:-} ]]; then
  /usr/sbin/libvirtd -d
  /cleanup.py
  kill -9 $(cat /var/run/libvirtd.pid)
fi

# leftover socket prevents libvirt from initializing correctly
rm -f /var/lib/libvirt/qemu/capabilities.monitor.sock

if [[ ! ${VIRTLET_DISABLE_KVM:-} ]]; then
  chown root:kvm /dev/kvm
fi

/usr/sbin/libvirtd --listen -d

while ! nc -z -v -w1 localhost 16509 >& /dev/null; do
  echo >&1 "Waiting for libvirt..."
  sleep 0.3
done

PROTOCOL="${VIRTLET_DOWNLOAD_PROTOCOL:-https}"

if [[ ${1:-} != -novirtlet ]]; then
    FLEXVOLUME_DIR=/usr/libexec/kubernetes/kubelet-plugins/volume/exec
    if [ ! -d ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver ]; then
      mkdir ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver
      cp /flexvolume_driver ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver/flexvolume_driver
    fi
    /usr/local/bin/virtlet -v=${VIRTLET_LOGLEVEL:-2} -logtostderr=true -libvirt-uri=qemu+tcp://localhost/system -image-download-protocol="${PROTOCOL}"
fi
