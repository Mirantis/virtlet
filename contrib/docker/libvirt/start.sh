#!/bin/bash

set -o errexit

modprobe kvm

chown root:root /etc/libvirt/libvirtd.conf
chown root:root /etc/libvirt/qemu.conf
chmod 644 /etc/libvirt/libvirtd.conf
chmod 644 /etc/libvirt/qemu.conf

exec /usr/sbin/libvirtd --listen
