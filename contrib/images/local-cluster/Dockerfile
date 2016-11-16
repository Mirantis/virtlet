FROM golang:1.7.3
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>

# sudo is needed for local-up-cluster
# Fake docker executable is only needed to fool local-up-cluster, we
# don't really need docker inside the container
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y sudo rsync && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* && \
    mkdir -p /go/src/k8s.io && \
    echo -e '#!/bin/sh\nexit 0' >/usr/bin/docker && chmod +x /usr/bin/docker && \
    go get -u github.com/jteeuwen/go-bindata/go-bindata && \
    git clone -b v1.5.0-alpha.1 https://github.com/kubernetes/kubernetes.git /go/src/k8s.io/kubernetes

WORKDIR /go/src/k8s.io/kubernetes

RUN hack/install-etcd.sh && \
    make WHAT="cmd/kubectl cmd/hyperkube"

COPY start.sh /
RUN chmod +x /start.sh

# test container should be able to run kubectl from there
VOLUME ["/go/src/k8s.io/kubernetes"]

EXPOSE 8080

CMD ["/start.sh"]
