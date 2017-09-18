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
IMAGE_TRANSLATIONS_DIR="${IMAGE_TRANSLATIONS_DIR:-}"

FLEXVOLUME_DIR=/usr/libexec/kubernetes/kubelet-plugins/volume/exec
if [ ! -d ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver ]; then
    mkdir ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver
    cp /flexvolume_driver ${FLEXVOLUME_DIR}/virtlet~flexvolume_driver/flexvolume_driver
fi


while [ ! -S /var/run/libvirt/libvirt-sock ] ; do
  echo >&1 "Waiting for libvirt..."
  sleep 0.3
done

/usr/local/bin/virtlet -v=${VIRTLET_LOGLEVEL:-3} -logtostderr=true -image-download-protocol="${PROTOCOL}" -image-translations-dir="${IMAGE_TRANSLATIONS_DIR}" "${RAW_DEVICES}"
