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
vmssh="${SCRIPT_DIR}/../../examples/vmssh.sh"

# provide path for kubectl
export PATH="${HOME}/.kubeadm-dind-cluster:${PATH}"

while ! "${virsh}" list --name | grep -q 'cirros-vm$'; do
  sleep 1
done

cd "${SCRIPT_DIR}"
"${SCRIPT_DIR}/vmchat.exp" 1

# test ceph RBD
vm_hostname="$("${vmssh}" cirros@cirros-vm cat /etc/hostname)"
expected_hostname=my-cirros-vm
if [[ "${vm_hostname}" != "${expected_hostname}" ]]; then
  echo "Unexpected vm hostname: ${vm_hostname} instead ${expected_hostname}" >&2
  exit 1
fi

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

# wait for login prompt to appear
"${SCRIPT_DIR}/vmchat-short.exp" 2

"${vmssh}" cirros@cirros-vm-rbd 'sudo /usr/sbin/mkfs.ext2 /dev/vdb && sudo mount /dev/vdb /mnt && ls -l /mnt | grep lost+found'

# check vnc consoles are available for both domains
if ! kubectl exec "${virtlet_pod_name}" --namespace=kube-system -- /bin/sh -c "apt-get install -y vncsnapshot"; then
  echo "Failed to install vncsnapshot inside virtlet container" >&2
  exit 1
fi

# grab screenshots

if ! kubectl exec "${virtlet_pod_name}" --namespace=kube-system -- /bin/sh -c "vncsnapshot :0 /domain_1.jpeg"; then
  echo "Failed to addtach and get screenshot for vnc console for domain with 1 id" >&2
  exit 1
fi

if ! kubectl exec "${virtlet_pod_name}" --namespace=kube-system -- /bin/sh -c "vncsnapshot :1 /domain_2.jpeg"; then
  echo "Failed to addtach and get screenshot for vnc console for domain with 2 id" >&2
  exit 1
fi

# check cpu count

function verify-cpu-count {
  local expected_count="${1}"
  cirros_cpu_count="$("${SCRIPT_DIR}/../../examples/vmssh.sh" cirros@cirros-vm grep '^processor' /proc/cpuinfo|wc -l)"
  if [[ ${cirros_cpu_count} != ${expected_count} ]]; then
    echo "bad cpu count for cirros-vm: ${cirros_cpu_count} instead of ${expected_count}" >&2
    exit 1
  fi
}

verify-cpu-count 1

# test pod removal

kubectl delete pod cirros-vm
n=180
while kubectl get pod cirros-vm >&/dev/null; do
  if ((--n == 0)); then
    echo "Timed out waiting for pod removal" >&2
  fi
  sleep 1
  echo -n "." >&2
done
echo

if "${virsh}" list --name|grep -- '-cirros-vm$'; then
  echo "cirros-vm domain still listed after deletion" >&2
  exit 1
fi

# test changing vcpu count

kubectl convert -f "${SCRIPT_DIR}/../../examples/cirros-vm.yaml" --local -o json | docker exec -i kube-master jq '.metadata.annotations.VirtletVCPUCount = "2"' | kubectl create -f -

while ! "${virsh}" list --name | grep -q 'cirros-vm$'; do
  sleep 1
done

# wait for login prompt to appear
"${SCRIPT_DIR}/vmchat-short.exp" 3

verify-cpu-count 2
