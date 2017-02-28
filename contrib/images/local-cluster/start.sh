#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

ext_ip="$(ip route get 1 | awk '{print $NF;exit}')"
export PATH=${PATH}:/go/src/k8s.io/kubernetes/third_party/etcd
# make insecure port of apiserver accessible from outside
export KUBELET_HOST="$ext_ip"
export HOSTNAME_OVERRIDE="$ext_ip"
export API_HOST="$ext_ip"
export API_HOST_IP="$ext_ip"
export KUBELET_HOST="$ext_ip"
export KUBERNETES_PROVIDER=local
export EXPERIMENTAL_CRI=true
export CONTAINER_RUNTIME=remote
export CONTAINER_RUNTIME_ENDPOINT=/run/virtlet.sock
while true
do
   echo "Waiting for $CONTAINER_RUNTIME_ENDPOINT"
   [ -e "$CONTAINER_RUNTIME_ENDPOINT" ] && break
   sleep 2
done
hack/local-up-cluster.sh -o _output/local/bin/linux/amd64
