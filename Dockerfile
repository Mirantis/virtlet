FROM golang:alpine
MAINTAINER Michal Rostecki <mrostecki@mirantis.com>

RUN apk --no-cache add \
	alpine-sdk \
	autoconf \
	automake \
	glib-dev \
	libvirt-dev

RUN mkdir -p /go/src/github.com/Mirantis/virtlet
COPY . /go/src/github.com/Mirantis/virtlet

WORKDIR /go/src/github.com/Mirantis/virtlet

RUN ./autogen.sh \
	&& ./configure \
	&& make \
	&& make install \
	&& make clean

CMD ["/usr/local/bin/virtlet", "-logtostderr=true", "-libvirt-uri=qemu+tcp://libvirt/system", "-etcd-endpoint=http://etcd:2379"]
