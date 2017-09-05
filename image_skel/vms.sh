#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if lsmod | grep '\<kvm\>' > /dev/null &&
   ! ([[ -e /dev/kvm ]] || mknod /dev/kvm c 10 $(grep '\<kvm\>' /proc/misc | cut -d" " -f1)); then
  echo >&2 "warning: can't create /dev/kvm"
elif [[ -e /dev/kvm ]]; then
  chown libvirt-qemu.kvm /dev/kvm
fi

echo 'Defaults closefrom_override' > /etc/sudoers.d/virtlet-qemu

# rm the following (tmp fix):
chown -R root.root /etc/sudoers.d/

# mount --bind /hostdev /dev

echo "$$ $(cut -d' ' -f22 /proc/$$/stat)" >/var/lib/virtlet/vms.procfile
sleep Infinity
