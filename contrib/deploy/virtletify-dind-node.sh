#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [ $(uname) = Darwin ]; then
  readlinkf(){ perl -MCwd -e 'print Cwd::abs_path shift' "$1";}
else
  readlinkf(){ readlink -f "$1"; }
fi

cd "$(dirname "$(readlinkf "${BASH_SOURCE}")")/../.."

container="${1:-kube-node-1}"

build/cmd.sh build
build/cmd.sh copy

docker cp _output/criproxy kube-node-1:/usr/local/bin/

# kubeadm-dind-cluster specific node naming
node_name="$(docker exec "${container}" hostname --ip-address)"

docker exec -d "${container}" bash -c '/usr/local/bin/criproxy -v 3 -alsologtostderr -connect /var/run/dockershim.sock,virtlet:/run/virtlet.sock >& /tmp/criproxy.log'

# FIXME: --node-labels doesn't work here because of this issue:
# https://github.com/kubernetes/kubernetes/issues/28051
docker exec "${container}" sed -i \
       "s@'\$@ --node-labels=extraRuntime=virtlet --experimental-cri --container-runtime=mixed --container-runtime-endpoint=/run/criproxy.sock --image-service-endpoint=/run/criproxy.sock'@" \
       /etc/systemd/system/kubelet.service.d/20-hostname-override.conf
docker exec -i "${container}" systemctl stop kubelet
docker exec -i "${container}" bash -c 'docker ps -qa|xargs docker rm -fv'
docker exec -i "${container}" systemctl daemon-reload
docker exec -i "${container}" systemctl start kubelet

# Set node label because --node-labels doesn't update node labels after
# restarting kubelet (see above)
kubectl label node "${node_name}" extraRuntime=virtlet

# kubectl create -f contrib/deploy/virtlet-ds.yaml
