#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [[ -f /dind/virtlet ]]; then
  ln -fs /dind/virtlet /usr/local/bin/virtlet
fi

if [[ ${VIRTLET_RAW_DEVICES:-} ]]; then
  RAW_DEVICES="-raw-devices $VIRTLET_RAW_DEVICES"
else
  RAW_DEVICES=""
fi

PROTOCOL="${VIRTLET_DOWNLOAD_PROTOCOL:-https}"

FLEXVOLUME_DIR=/usr/libexec/kubernetes/kubelet-plugins/volume/exec
if [ ! -d ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver ]; then
    mkdir ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver
    cp /flexvolume_driver ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver/flexvolume_driver
fi


while ! nc -z -v -w1 localhost 16509 >& /dev/null; do
  echo >&1 "Waiting for libvirt..."
  sleep 0.3
done

/usr/local/bin/virtlet -v=${VIRTLET_LOGLEVEL:-2} -logtostderr=true -libvirt-uri=qemu+tcp://localhost/system -image-download-protocol="${PROTOCOL}" "${RAW_DEVICES}"
