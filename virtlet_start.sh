#!/bin/sh

FLANNEL_ENV_FILE=/run/flannel/subnet.env

ATTEMPS=0
MAX_ATTEMPS=5
TIME_BETWEEN=1

while true ; do
	if [ -f "$FLANNEL_ENV_FILE" ] ; then
		break
	fi

	sleep $TIME_BETWEEN

	ATTEMPS=$((ATTEMPS + 1))
	if [ $ATTEMPS -eq $MAX_ATTEMPS ] ; then
		echo "$FLANNEL_ENV_FILE not found after $ATTEMPS attemps to read it." >&2
		exit 1
	fi
done

source "$FLANNEL_ENV_FILE"
export FLANNEL_NETWORK FLANNEL_SUBNET FLANNEL_MTU FLANNEL_IPMASQ

/usr/local/bin/virtlet -logtostderr=true -libvirt-uri=qemu+tcp://libvirt/system -etcd-endpoint=http://etcd:2379
