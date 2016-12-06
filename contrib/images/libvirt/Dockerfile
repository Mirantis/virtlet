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
                netbase \
                ebtables \
	&& apt-get clean

COPY cleanup.py /cleanup.py
COPY start.sh /start.sh
COPY libvirt.conf /etc/libvirt/libvirtd.conf
COPY qemu.conf /etc/libvirt/qemu.conf
COPY _output/vmwrapper /

# uncomment to add vmwrapper hook scripts
# COPY vmwrapper-entry.sh /
COPY vmwrapper-pre-qemu.sh /
# COPY /vmwrapper-after-qemu.sh

CMD ["/start.sh"]
