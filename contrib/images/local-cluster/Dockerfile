FROM golang:1.7.3
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>
ARG k8s_version=v1.5.1

# sudo is needed for local-up-cluster
# Fake docker executable is only needed to fool local-up-cluster, we
# don't really need docker inside the container.
# Also we have to work around hack/local-up-cluster.sh problem in v1.5.1,
# see https://github.com/kubernetes/kubernetes/issues/38847 by adding
# --anonymous-auth=true to apiserver invocation inside
# hack/local-up-cluster.sh script.
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y sudo rsync iptables && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* && \
    mkdir -p /go/src/k8s.io && \
    echo -e '#!/bin/sh\nexit 0' >/usr/bin/docker && chmod +x /usr/bin/docker && \
    go get -u github.com/jteeuwen/go-bindata/go-bindata && \
    git clone -b ${k8s_version} https://github.com/kubernetes/kubernetes.git /go/src/k8s.io/kubernetes && \
    sed -i 's@sudo -E "\${GO_OUT}/hyperkube" apiserver@sudo -E "${GO_OUT}/hyperkube" apiserver --anonymous-auth=true@' /go/src/k8s.io/kubernetes/hack/local-up-cluster.sh

WORKDIR /go/src/k8s.io/kubernetes

RUN hack/install-etcd.sh && \
    make WHAT="cmd/kubectl cmd/hyperkube"

COPY start.sh /
RUN chmod +x /start.sh

# test container should be able to run kubectl from there
VOLUME ["/go/src/k8s.io/kubernetes"]

EXPOSE 8080

CMD ["/start.sh"]
