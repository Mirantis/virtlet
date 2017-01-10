FROM ubuntu:16.04
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>
LABEL Name="e2e-test" Version="latest"

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive \
      apt-get install -y python-minimal wget iproute2 libvirt-bin expect ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* && \
    mkdir /images && \
    wget https://github.com/eprasad/virt-cirros/raw/master/virt-cirros-0.3.4-x86_64-disk.img -O /images/cirros

COPY run.sh vmchat.exp /
RUN chmod +x /run.sh /vmchat.exp

CMD ["/run.sh"]
