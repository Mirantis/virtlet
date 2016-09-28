FROM ubuntu:16.04
MAINTAINER Michal Rostecki <mrostecki@mirantis.com>

RUN apt-get update && apt-get install -y software-properties-common
RUN add-apt-repository ppa:ubuntu-lxc/lxd-stable
RUN apt-get update && apt-get install -y golang

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"
WORKDIR $GOPATH

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y  \
	make \
	autoconf \
	automake \
	libglib2.0-dev \
	libvirt-dev \
	libguestfs-dev \
	libguestfs0-dbg \
	libguestfs-tools

RUN mkdir -p /go/src/github.com/Mirantis/virtlet
COPY . /go/src/github.com/Mirantis/virtlet

WORKDIR /go/src/github.com/Mirantis/virtlet

RUN ./autogen.sh \
	&& ./configure \
	&& make \
	&& make install \
	&& make clean

CMD ["/usr/local/bin/virtlet", "-logtostderr=true", "-libvirt-uri=qemu+tcp://libvirt/system", "-etcd-endpoint=http://etcd:2379"]
