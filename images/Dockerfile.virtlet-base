FROM ubuntu:16.04
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>

# BUMP 23.11.2018

ENV DEBIAN_FRONTEND noninteractive

RUN echo deb-src http://archive.ubuntu.com/ubuntu/ xenial main universe restricted >>/etc/apt/sources.list && \
    echo deb-src http://archive.ubuntu.com/ubuntu/ xenial-updates main universe restricted >>/etc/apt/sources.list

RUN apt-get -y update && \
    apt-get -y build-dep libguestfs && \
    apt-get -y build-dep supermin && \
    apt-get -y install git libjansson-dev libhivex-ocaml-dev

RUN git clone https://github.com/libguestfs/supermin.git && \
    cd supermin && \
    git checkout v5.1.19 && \
    ./bootstrap && \
    ./autogen.sh --prefix=/usr/local && \
    make -j$(grep -c ^processor /proc/cpuinfo) install

RUN git clone https://github.com/libguestfs/libguestfs.git && \
    cd libguestfs && \
    git checkout v1.39.1 && \
    ./autogen.sh --prefix=/usr/local && \
    make -j$(grep -c ^processor /proc/cpuinfo); rm po-docs/podfiles && \
    make -C po-docs update-po -j$(grep -c ^processor /proc/cpuinfo) && \
    make -j$(grep -c ^processor /proc/cpuinfo) install REALLY_INSTALL=yes

FROM ubuntu:16.04
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>

LABEL virtlet.image="virtlet-base"

COPY --from=0 /usr/local /usr/local

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update && apt-get install -y curl && \
    echo deb http://mirror.mirantis.com/stable/openstack-queens/xenial xenial main >>/etc/apt/sources.list && \
    curl http://mirror.mirantis.com/stable/openstack-queens/xenial/archive-queens.key | apt-key add - && \
    apt-get update && \
    apt-get install -y libvirt-bin libvirt-daemon libvirt-dev bridge-utils \
                       openssl qemu-kvm \
                       netbase iptables ebtables vncsnapshot \
                       socat netcat-openbsd \
                       acl attr binutils bsdmainutils btrfs-tools \
                       bzip2 cpio cryptsetup curl dosfstools extlinux \
                       file gawk gdisk genisoimage iproute iproute2 \
                       isc-dhcp-client kmod less libaugeas0 \
                       libavahi-client3 libavahi-common3 libcap-ng0 \
                       libcurl3-gnutls libdbus-1-3 libfuse2 libgnutls30 \
                       libhivex0 libmagic1 libnl-3-200 \
                       libnuma1 libsasl2-2 libxen-4.6 libxml2 libyajl2 \
                       lsscsi lvm2 lzop mdadm module-init-tools \
                       mtools ntfs-3g openssh-client parted psmisc \
                       qemu-system-x86 qemu-utils scrub syslinux \
                       udev xz-utils zerofree libjansson4 \
                       dnsmasq libpcap0.8 libnetcf1 dmidecode && \
    apt-get clean

# TODO: try to go back to alpine
# TODO: check which libs are really needed for libvirt / libguestfs / supermin
# and which aren't
