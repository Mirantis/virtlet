FROM ubuntu:16.04
MAINTAINER Michal Rostecki <mrostecki@mirantis.com>
LABEL Name="virtlet" Version="0.1"

RUN apt-get update \
	&& DEBIAN_FRONTEND=noninteractive apt-get install -y \
                git \
		golang \
		make \
		autoconf \
		automake \
		libglib2.0-dev \
		libvirt-dev \
		libguestfs-dev \
		libguestfs0-dbg \
		libguestfs-tools \
		iptables \
	&& apt-get clean

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"
WORKDIR $GOPATH

RUN mkdir -p /go/src/github.com/Mirantis/virtlet
COPY . /go/src/github.com/Mirantis/virtlet

WORKDIR /go/src/github.com/Mirantis/virtlet

RUN mkdir -p ~/.ssh \
	&& ssh-keyscan github.com >> ~/.ssh/known_hosts

RUN ./autogen.sh \
	&& ./configure \
	&& make \
	&& make install \
	&& make clean

RUN mkdir -p /var/data/virtlet

CMD ["/bin/bash", "-c", "/usr/local/bin/virtlet -v=${VIRTLET_LOGLEVEL:-2} -logtostderr=true -libvirt-uri=qemu+tcp://libvirt/system"]
