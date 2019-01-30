# TODO: generate this tag. unfortunately can't use ARG:
# https://docs.docker.com/engine/reference/builder/#understand-how-arg-and-from-interact
# (but add a note about it here for the future)
FROM mirantis/virtlet-build:v1-a41085a8556b19927a08548c91d3f1bb
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>

LABEL virtlet.image="build"

RUN mkdir -p /go/src/github.com/Mirantis/virtlet
WORKDIR /go/src/github.com/Mirantis/virtlet

COPY image_skel /.
# this conf file runs the emulator as root which is ok for
# testing purposes
COPY qemu-build.conf /etc/libvirt/qemu.conf
