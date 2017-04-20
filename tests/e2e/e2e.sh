#!/bin/bash
# Copyright 2017 Mirantis
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

if [ $(uname) = Darwin ]; then
  readlinkf(){ perl -MCwd -e 'print Cwd::abs_path shift' "$1";}
else
  readlinkf(){ readlink -f "$1"; }
fi

SCRIPT_DIR="$(cd $(dirname "$(readlinkf "${BASH_SOURCE}")"); pwd)"
virsh="${SCRIPT_DIR}/../../examples/virsh.sh"

# provide path for kubectl
export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"

while ! "${virsh}" list | grep -q cirros-vm; do
  sleep 1
done

cd "${SCRIPT_DIR}"
"${SCRIPT_DIR}/vmchat.exp" $("${virsh}" list --name)

virtlet_pod_name=$(kubectl get pods --namespace=kube-system | grep virtlet | awk '{print $1}')

# Run one-node ceph cluster
"${SCRIPT_DIR}/run_ceph.sh" "${SCRIPT_DIR}"
kubectl create -f "${SCRIPT_DIR}/cirros-vm-rbd-volume.yaml"
while ! "${virsh}" list | grep -q cirros-vm-rbd; do
  sleep 1
done
if [ "$(${virsh} domblklist 2 | grep rbd-test-image | wc -l)" != "1" ]; then
  exit 1
fi
if ! kubectl exec "${virtlet_pod_name}" --namespace=kube-system -- /bin/sh -c "virsh list | grep cirros-vm-rbd.*running"; then
  exit 1
fi

# check vnc consoles are available for both domains
if ! kubectl exec "${virtlet_pod_name}" --namespace=kube-system -- /bin/sh -c "apt-get install -y vncsnapshot"; then
  echo "Failed to install vncsnapshot inside virtlet container"
  exit 1
fi

if ! kubectl exec "${virtlet_pod_name}" --namespace=kube-system -- /bin/sh -c "vncsnapshot :0 /domain_1.jpeg"; then
  echo "Failed to addtach and get screenshot for vnc console for domain with 1 id"
  exit 1
fi

if ! kubectl exec "${virtlet_pod_name}" --namespace=kube-system -- /bin/sh -c "vncsnapshot :1 /domain_2.jpeg"; then
  echo "Failed to addtach and get screenshot for vnc console for domain with 2 id"
  exit 1
fi
