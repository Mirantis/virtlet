FROM golang:alpine
MAINTAINER Michal Rostecki <mrostecki@mirantis.com>

RUN apk --no-cache add \
	alpine-sdk \
	libvirt-dev

RUN mkdir -p /go/src/github.com/Mirantis/virtlet
COPY . /go/src/github.com/Mirantis/virtlet

WORKDIR /go/src/github.com/Mirantis/virtlet

RUN make \
	&& make install \
	&& make clean

CMD ["/usr/local/bin/virtlet", "-logtostderr=true", "-libvirt-uri=qemu+tcp://0.0.0.0/system", "-etcd-endpoint=http://0.0.0.0:2379"]
