FROM centos:7
MAINTAINER Michal Rostecki <mrostecki@mirantis.com>

RUN yum -y install \
		openssl \
		libvirt-daemon \
		libvirt-python \
		libguestfs \
		libvirt-daemon-driver-nwfilter \
		libvirt-daemon-config-nwfilter \
		qemu-kvm \
		ceph-common \
	&& yum clean all

COPY cleanup.py /cleanup.py
COPY start.sh /start.sh

CMD ["/start.sh"]
