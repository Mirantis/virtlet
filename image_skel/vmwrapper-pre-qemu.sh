#!/bin/bash -x

echo >&2 "pre qemu setup"
# XXX: do this in vm network setup
ip link set lo up
ip a
echo >&2 "pre qemu setup done"

cat /proc/$$/status >&2
id >&2
cat /etc/libvirt/qemu.conf >&2
ls -l /var/lib >&2
ls -l /var/lib/libvirt >&2
ls -l /var/lib/libvirt/qemu/ >&2

# tcpdump -i br0 >& /tmp/tcpdump.log&
