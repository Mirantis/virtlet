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

docker exec kube-node-1 mkdir -p /opt/criproxy/bin
docker cp _output/criproxy kube-node-1:/opt/criproxy/bin/criproxy

# kubeadm-dind-cluster specific node naming
node_name="$(docker exec "${container}" hostname --ip-address)"

kubectl label node "${node_name}" extraRuntime=virtlet

# kubectl create -f deploy/virtlet-ds.yaml
