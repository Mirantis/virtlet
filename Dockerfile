FROM ubuntu:16.04
# TODO: try to go back to alpine
MAINTAINER Ivan Shvedunov <ivan4th@gmail.com>
LABEL Name="virtlet" Version="0.1"

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update && \
    apt-get install -y libvirt-bin libguestfs0 libguestfs-tools ceph-common \
                       openssl qemu-kvm qemu-system-x86 python-libvirt \
                       netbase iproute2 iptables ebtables && \
    apt-get clean

RUN mkdir -p /var/data/virtlet /var/lib/virtlet /opt/cni/bin && \
    curl -L https://github.com/containernetworking/cni/releases/download/v0.3.0/cni-v0.3.0.tgz | \
      tar zxC /opt/cni/bin

# Integration tests look for virtlet in $PATH
# and we want it to be located in the same place both
# in build/test image and production one
COPY _output/virtlet /usr/local/bin
COPY _output/vmwrapper /

COPY image_skel /.

CMD ["/start.sh"]
