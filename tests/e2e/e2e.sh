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

function wait-for-pod {
  local pod="${1}"
  local n=180
  while true; do
    local phase="$(kubectl get pod "${1}" -o jsonpath='{.status.phase}')"
    if [[ ${phase} == Running ]]; then
      break
    fi
    if ((--n == 0)); then
      echo "Timed out waiting for pod ${pod}" >&2
      exit 1
    fi
    sleep 1
    echo -n "." >&2
  done
  echo >&2
}

function wait-for-ssh {
  local vmname=${1}
  local retries=10
  # dropbear inside cirros vm on Travis (thus non-KVM) is shaky
  for ((i = 0; i < 4; i++)); do
    while ! ../../examples/vmssh.sh "cirros@${vmname}" "echo Hello ${1}" | grep -q "Hello ${1}"; do
      if ((--retries <= 0));then
        echo "Timed out waiting for ssh to ${vmname}"
        exit 1
      fi
      echo "Waiting for ssh to ${vmname}"
      sleep 1
    done
  done
}

function delete-pod-and-wait {
  local pod="${1}"
  kubectl delete pod "${pod}"
  n=180
  while kubectl get pod "${pod}" >&/dev/null; do
    if ((--n == 0)); then
      echo "Timed out waiting for pod removal" >&2
      exit 1
    fi
    sleep 1
    echo -n "." >&2
  done
  echo >&2
  if "${virsh}" list --name|grep -- '-${pod}$'; then
    echo "${pod} domain still listed after deletion" >&2
    exit 1
  fi
}

function vmchat-short {
  local vmname=${1}
  wait-for-ssh ${vmname}

  count=$(../../examples/vmssh.sh "cirros@${vmname}" "ip a" | grep "eth0:" | wc -l)
  if [[ ${count} != 1 ]]; then
    echo "Executing 'ip a' failed. Expected 1 line but got ${count}"
    exit 1
  fi
}

function vmchat {
  local vmname=${1}
  vmchat-short ${vmname}

  count=$(../../examples/vmssh.sh "cirros@${vmname}" "ip r" | grep "default via" | wc -l)
  if [[ ${count} != 1 ]]; then
    echo "Executing 'ip r' failed. Expected 1 line but got ${count}"
    exit 1
  fi

  count=$(../../examples/vmssh.sh "cirros@${vmname}" "ping -c1 8.8.8.8" | grep "1 packets transmitted, 1 packets received, 0% packet loss" | wc -l)
  if [[ ${count} != 1 ]]; then
    echo "Executing 'ping -c1 8.8.8.8' failed. Expected 1 line but got ${count}"
    exit 1
  fi

  count=$(../../examples/vmssh.sh "cirros@${vmname}" "curl http://nginx.default.svc.cluster.local" | grep "Thank you for using nginx." | wc -l)
  if [[ ${count} != 1 ]]; then
    echo "Executing 'curl http://nginx.default.svc.cluster.local' failed. Expected 1 line but got ${count}"
    exit 1
  fi
}

wait-for-pod cirros-vm

cd "${SCRIPT_DIR}"
vmchat cirros-vm

# test logging

virshid=$($virsh list | grep "\-cirros-vm " | cut -f2 -d " ")
logpath=$($virsh dumpxml $virshid | xmllint --xpath 'string(//serial[@type="file"]/source/@path)' -)
filename=$(echo $logpath | sed -E 's#.+/##')
sandboxid=$(echo $logpath | sed 's#/var/log/vms/##' | sed -E 's#/.+##')
nodeid=$(docker ps | grep kube-master | cut -f1 -d " ")

count=$(docker exec -it ${nodeid} /bin/bash -c "cat /var/log/virtlet/vms/${sandboxid}/${filename}" | \
     grep "login as 'cirros' user. default password: 'cubswin:)'. use 'sudo' for root." | \
     wc -l)
if [[ ${count} != 1 ]]; then
  echo "Checking raw log file failed. Expected 1 line but got ${count}"
  exit 1
fi

# DIND containers have jq installed so we can use it
count=$(docker exec -it ${nodeid} /bin/bash -c "jq .log /var/log/pods/${sandboxid}/${filename}" | \
     grep "login as 'cirros' user. default password:.*'. use 'sudo' for root" | \
     wc -l)
if [[ ${count} != 1 ]]; then
  echo "Checking formatted log file failed. Expected 1 line but got ${count}"
  exit 1
fi

vm_hostname="$("${vmssh}" cirros@cirros-vm cat /etc/hostname)"
expected_hostname=cirros-vm
if [[ "${vm_hostname}" != "${expected_hostname}" ]]; then
  echo "Unexpected vm hostname: ${vm_hostname} instead ${expected_hostname}" >&2
  exit 1
fi

# test ceph RBD

virtlet_pod_name=$(kubectl get pods --namespace=kube-system | grep -v virtlet-log | grep virtlet | awk '{print $1}')

# Run one-node ceph cluster
"${SCRIPT_DIR}/run_ceph.sh" "${SCRIPT_DIR}"

# check attaching RBD device that's specified in the pod definition
kubectl create -f "${SCRIPT_DIR}/cirros-vm-rbd-volume.yaml"
wait-for-pod cirros-vm-rbd
if [ "$(${virsh} domblklist @cirros-vm-rbd | grep rbd-test-image$ | wc -l)" != "1" ]; then
  echo "ceph: failed to find rbd-test-image in domblklist" >&2
  exit 1
fi

# check attaching rbd device specified using PV/PVC
# tmp workaround: clear secret
secretUUID=$(${virsh} secret-list | grep ceph | awk '{print $1}')
if [[ ${secretUUID} ]]; then
  if ! ${virsh} secret-undefine ${secretUUID} >&/dev/null; then
    echo "ceph: failed to clear secret"
    exit 1
  fi
fi

kubectl create -f "${SCRIPT_DIR}/cirros-vm-rbd-pv-volume.yaml"
wait-for-pod cirros-vm-rbd-pv
if [ "$(${virsh} domblklist @cirros-vm-rbd-pv | grep rbd-test-image-pv$ | wc -l)" != "1" ]; then
  echo "ceph: failed to find rbd-test-image-pv in domblklist" >&2
  exit 1
fi

# wait for login prompt to appear
vmchat-short cirros-vm-rbd

"${vmssh}" cirros@cirros-vm-rbd 'sudo /usr/sbin/mkfs.ext2 /dev/vdb && sudo mount /dev/vdb /mnt && ls -l /mnt | grep lost+found'

# check vnc consoles are available for both domains
if ! kubectl exec "${virtlet_pod_name}" -c virtlet --namespace=kube-system -- /bin/sh -c "apt-get install -y vncsnapshot"; then
  echo "Failed to install vncsnapshot inside virtlet container" >&2
  exit 1
fi

# grab screenshots

if ! kubectl exec "${virtlet_pod_name}" -c virtlet --namespace=kube-system -- /bin/sh -c "vncsnapshot :0 /domain_1.jpeg"; then
  echo "Failed to attach and get screenshot for vnc console for domain with 1 id" >&2
  exit 1
fi

if ! kubectl exec "${virtlet_pod_name}" -c virtlet --namespace=kube-system -- /bin/sh -c "vncsnapshot :1 /domain_2.jpeg"; then
  echo "Failed to attach and get screenshot for vnc console for domain with 2 id" >&2
  exit 1
fi

# check cpu count

function verify-cpu-count {
  local expected_count="${1}"
  cirros_cpu_count="$("${vmssh}" cirros@cirros-vm grep '^processor' /proc/cpuinfo|wc -l)"
  if [[ ${cirros_cpu_count} != ${expected_count} ]]; then
    echo "bad cpu count for cirros-vm: ${cirros_cpu_count} instead of ${expected_count}" >&2
    exit 1
  fi
}

verify-cpu-count 1

# test pod removal

delete-pod-and-wait cirros-vm

# test changing vcpu count

kubectl convert -f "${SCRIPT_DIR}/../../examples/cirros-vm.yaml" --local -o json | jq '.metadata.annotations.VirtletVCPUCount = "2" | .spec.containers[0].resources.limits.cpu = "500m"' | kubectl create -f -

wait-for-pod cirros-vm

# wait for login prompt to appear
vmchat-short cirros-vm

verify-cpu-count 2

# verify domain memory size settings

function domain_xpath {
  local domain="${1}"
  local xpath="${2}"
  kubectl exec -n kube-system "${virtlet_pod_name}" -c virtlet -- \
          /bin/sh -c "virsh dumpxml '${domain}' | xmllint --xpath '${xpath}' -"
}

pod_domain="$("${virsh}" poddomain @cirros-vm)"

# <cputune>
#    <period>100000</period>
#    <quota>25000</quota>
# </cputune>
expected_dom_quota="25000"
expected_dom_period="100000"

dom_quota="$(domain_xpath "${pod_domain}" 'string(/domain/cputune/quota)')"
dom_period="$(domain_xpath "${pod_domain}" 'string(/domain/cputune/period)')"

if [[ ${dom_quota} != ${expected_dom_quota} ]]; then
  echo "Bad quota value in the domain definition. Expected ${dom_quota}, but got ${expected_dom_quota}" >&2
  exit 1
fi

if [[ ${dom_period} != ${expected_dom_period} ]]; then
  echo "Bad period value in the domain definition. Expected ${dom_period}, but got ${expected_dom_period}" >&2
  exit 1
fi

# <memory unit='KiB'>131072</memory>
dom_mem_size_k="$(domain_xpath "${pod_domain}" 'string(/domain/memory[@unit="KiB"])')"
expected_dom_mem_size_k="131072"
if [[ ${dom_mem_size_k} != ${expected_dom_mem_size_k} ]]; then
  echo "Bad memory size in the domain definition. Expected ${dom_mem_size_k}, but got ${expected_mem_size_k}" >&2
  exit 1
fi

# verify <memoryBacking><locked/></memoryBacking> in the domain definition
# (so the VM memory doesn't get swapped out)

if [[ $(domain_xpath "${pod_domain}" 'count(/domain/memoryBacking/locked)') != 1 ]]; then
  echo "Didn't find memoryBacking/locked in the domain definition" >&2
  exit 1
fi

# verify memory size as reported by Linux kernel inside VM

# The boot message is:
# [    0.000000] Memory: 109112k/130944k available (6576k kernel code, 452k absent, 21380k reserved, 6620k data, 928k init)

mem_size_k="$("${vmssh}" cirros@cirros-vm dmesg|grep 'Memory:'|sed 's@.*/\|k .*@@g')"
expected_mem_size_k=130944

if [[ ${mem_size_k} != ${expected_mem_size_k} ]]; then
  echo "Bad memory size (inside VM). Expected ${expected_mem_size_k}, but got ${mem_size_k}" >&2
  exit 1
fi

# Try stopping hung vm. We make VM hang by invoking 'halt -nf' from
# cloud-init userdata
kubectl convert -f "${SCRIPT_DIR}/../../examples/cirros-vm.yaml" --local -o json |
    jq '.metadata.name="haltme"|.metadata.annotations.VirtletCloudInitUserDataScript="#!/bin/sh\n/sbin/halt -nf"' |
    kubectl create -f -
wait-for-pod haltme
# FIXME: it would be better to halt the VM over ssh + wait for it to
# stop receiving pings probably. We can do it after rewriting e2e
# tests in Go.
sleep 15
delete-pod-and-wait haltme
