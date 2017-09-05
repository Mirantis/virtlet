#!/bin/bash
nspid="$1"
shift
exec sudo -C 10000 nsenter -t ${nspid} -m -u -i -n sudo -C 10000 -u libvirt-qemu "$@"
