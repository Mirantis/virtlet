FROM ubuntu:16.04
MAINTAINER Michal Rostecki <mrostecki@mirantis.com>

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y \
		iproute2 \
		openssl \
		qemu-kvm \
		libvirt-bin \
		python-libvirt \
		ceph-common \
	&& apt-get clean

COPY cleanup.py /cleanup.py
COPY start.sh /start.sh

CMD ["/start.sh"]
