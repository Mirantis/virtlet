FROM ubuntu:16.04
MAINTAINER Michal Rostecki <mrostecki@mirantis.com>
LABEL Name="libvirt" Version="latest"

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y \
		iproute2 \
		openssl \
		qemu-kvm \
                qemu-system-x86 \
		libvirt-bin \
		python-libvirt \
		ceph-common \
	&& apt-get clean

COPY cleanup.py /cleanup.py
COPY start.sh /start.sh

CMD ["/start.sh"]
