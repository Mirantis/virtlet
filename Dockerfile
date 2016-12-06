FROM ubuntu:16.04
# TODO: try to go back to alpine
MAINTAINER Ivan Shvedunov <ivan4th@gmail.com>
LABEL Name="virtlet" Version="0.1"

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update && \
    apt-get install -y libvirt0 libguestfs0 libguestfs-tools iptables && \
    apt-get clean

RUN mkdir -p /var/data/virtlet
COPY _output/virtlet /usr/local/bin/

ADD contrib/images/cni/etc /etc
ADD contrib/images/cni/opt /opt

CMD ["/bin/bash", "-c", "/usr/local/bin/virtlet -v=${VIRTLET_LOGLEVEL:-2} -logtostderr=true -libvirt-uri=qemu+tcp://libvirt/system"]
