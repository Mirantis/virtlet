FROM ubuntu:16.04
# TODO: try to go back to alpine
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update && \
    apt-get install -y libvirt-bin libguestfs0 libguestfs-tools genisoimage \
                       openssl qemu-kvm qemu-system-x86 python-libvirt \
                       netbase iproute2 iptables ebtables vncsnapshot && \
    apt-get clean

RUN mkdir -p /var/lib/virtlet/volumes /opt/cni/bin && \
    curl -L https://github.com/containernetworking/cni/releases/download/v0.3.0/cni-v0.3.0.tgz | \
      tar zxC /opt/cni/bin

COPY image_skel /.
COPY _output/flexvolume_driver /

CMD ["/start.sh"]

# Integration tests look for virtlet in $PATH
# and we want it to be located in the same place both
# in build/test image and production one
COPY _output/virtlet /usr/local/bin
COPY _output/vmwrapper /
COPY _output/criproxy /
COPY _output/virtlet_log /
COPY _output/virtlet-e2e-tests /
