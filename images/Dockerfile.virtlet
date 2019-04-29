# TODO: generate this tag. unfortunately can't use ARG:
# https://docs.docker.com/engine/reference/builder/#understand-how-arg-and-from-interact
# (but add a note about it here for the future)
FROM mirantis/virtlet-base:v1-6348ee2277c565d3895260bccb5ada96
MAINTAINER Ivan Shvedunov <ishvedunov@mirantis.com>

LABEL virtlet.image="virtlet"

COPY image_skel /.
COPY _output/flexvolume_driver /
# Integration tests look for virtlet in $PATH
# and we want it to be located in the same place both
# in build/test image and production one
COPY _output/virtlet /usr/local/bin
COPY _output/virtletctl /usr/local/bin
COPY _output/virtlet-longevity-tests /usr/local/bin
COPY _output/vmwrapper /
COPY _output/virtlet-e2e-tests /
RUN GRPC_HEALTH_PROBE_VERSION=v0.2.2 && \
    curl -L -s -o /usr/local/bin/grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
    chmod +x /usr/local/bin/grpc_health_probe

CMD ["/start.sh"]
