#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

NONINTERACTIVE="${NONINTERACTIVE:-}"
NO_VM_CONSOLE="${NO_VM_CONSOLE:-}"
INJECT_LOCAL_IMAGE="${INJECT_LOCAL_IMAGE:-}"
dind_script="dind-cluster-v1.6.sh"
kubectl="${HOME}/.kubeadm-dind-cluster/kubectl"
BASE_LOCATION="${BASE_LOCATION:-https://raw.githubusercontent.com/Mirantis/virtlet/master/}"
# Convenience setting for local testing:
# BASE_LOCATION="${HOME}/work/kubernetes/src/github.com/Mirantis/virtlet"
cirros_key="demo-cirros-private-key"

function demo::step {
  local OPTS=""
  if [ "$1" = "-n" ]; then
    shift
    OPTS+="-n"
  fi
  GREEN="$1"
  shift
  if [ -t 2 ] ; then
    echo -e ${OPTS} "\x1B[97m* \x1B[92m${GREEN}\x1B[39m $*" >&2
  else
    echo ${OPTS} "* ${GREEN} $*" >&2
  fi
}

function demo::ask-before-continuing {
  if [[ ! ${NONINTERACTIVE} ]]; then
    echo "Press Enter to continue or Ctrl-C to stop." >&2
    read
  fi
}

function demo::get-dind-cluster {
  if [[ -f ${dind_script} ]]; then
    demo::step "Will now clear existent ${dind_script} to be sure it is up to date"
    demo::ask-before-continuing
    rm "${dind_script}"
  fi
  demo::step "Will download dind-cluster-v1.6.sh into current directory"
  demo::ask-before-continuing
  wget "https://raw.githubusercontent.com/Mirantis/kubeadm-dind-cluster/master/fixed/${dind_script}"
  chmod +x "${dind_script}"
}

function demo::get-cirros-ssh-keys {
  if [[ -f ${cirros_key} ]]; then
    return 0
  fi
  demo::step "Will download ${cirros_key} into current directory"
  wget -O ${cirros_key} "https://raw.githubusercontent.com/Mirantis/virtlet/master/examples/vmkey"
  chmod 600 ${cirros_key}
}

function demo::start-dind-cluster {
  demo::step "Will now clear any kubeadm-dind-cluster data on the current Docker"
  if [[ ! ${NONINTERACTIVE} ]]; then
    echo "Cirros ssh connection will be open after Virtlet setup is complete, press Ctrl-D to disconnect." >&2
  fi
  echo "To clean up the cluster, use './dind-cluster-v1.6.sh clean'" >&2
  demo::ask-before-continuing
  "./${dind_script}" clean
  # use zero-worker configuration for faster startup
  NUM_NODES=0 "./${dind_script}" up
}

function demo::inject-local-image {
  demo::step "Copying local mirantis/virtlet image into kube-master container"
  docker save mirantis/virtlet | docker exec -i kube-master docker load
}

function demo::label-node {
  demo::step "Applying label to kube-master:" "extraRuntime=virtlet"
  "${kubectl}" label node kube-master extraRuntime=virtlet
}

function demo::pods-ready {
  local label="$1"
  local out
  if ! out="$("${kubectl}" get pod -l "${label}" -n kube-system \
                           -o jsonpath='{ .items[*].status.conditions[?(@.type == "Ready")].status }' 2>/dev/null)"; then
    return 1
  fi
  if ! grep -v False <<<"${out}" | grep -q True; then
    return 1
  fi
  return 0
}

function demo::service-ready {
  local name="$1"
  if ! "${kubectl}" describe service -n kube-system "${name}"|grep -q '^Endpoints:.*[0-9]\.'; then
    return 1
  fi
}

function demo::wait-for {
  local title="$1"
  local action="$2"
  local what="$3"
  shift 3
  demo::step "Waiting for:" "${title}"
  while ! "${action}" "${what}" "$@"; do
    echo -n "." >&2
    sleep 1
  done
  echo "[done]" >&2
}

virtlet_pod=
function demo::virsh {
  local opts=
  if [[ ${1:-} = "console" ]]; then
    # using -it with `virsh list` causes it to use \r\n as line endings,
    # which makes it less useful
    local opts="-it"
  fi
  if [[ ! ${virtlet_pod} ]]; then
    virtlet_pod=$("${kubectl}" get pods -n kube-system -l runtime=virtlet -o name|head -1|sed 's@.*/@@')
  fi
  "${kubectl}" exec ${opts} -n kube-system "${virtlet_pod}" -c virtlet -- virsh "$@"
}

function demo::ssh {
  local cirros_ip=

  demo::get-cirros-ssh-keys

  if [[ ! ${virtlet_pod} ]]; then
    virtlet_pod=$("${kubectl}" get pods -n kube-system -l runtime=virtlet -o name|head -1|sed 's@.*/@@')
  fi

  if [[ ! ${cirros_ip} ]]; then
    while true; do
      cirros_ip=$(kubectl get pod cirros-vm -o jsonpath="{.status.podIP}")
      if [[ ! ${cirros_ip} ]]; then
        echo "Waiting for cirros IP..."
        sleep 1
        continue
      fi
      echo "Cirros IP is ${cirros_ip}."
      break
    done
  fi

  echo "Trying to establish ssh connection to cirros-vm..."
  while ! internal::ssh ${virtlet_pod} ${cirros_ip} "echo Hello" | grep -q "Hello"; do
    sleep 1
    echo "Trying to establish ssh connection to cirros-vm..."
  done

  echo "Successfully established ssh connection. Press Ctrl-D to disconnect."
  internal::ssh ${virtlet_pod} ${cirros_ip}
}

function internal::ssh {
  virtlet_pod=${1}
  cirros_ip=${2}
  shift 2

  ssh -oProxyCommand="${kubectl} exec -i -n kube-system ${virtlet_pod} -c virtlet -- nc -q0 ${cirros_ip} 22" \
    -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -q \
    -i ${cirros_key} cirros@cirros-vm "$@"
}

function demo::vm-ready {
  local name="$1"
  # note that the following is not a bulletproof check
  if ! demo::virsh list --name | grep -q "${name}\$"; then
    return 1
  fi
}

function demo::kvm-ok {
  demo::step "Checking for KVM support..."
  # The check is done inside kube-master container because it has proper /lib/modules
  # from the docker host. Also, it'll have to use mirantis/virtlet image
  # later anyway.
  if ! docker exec kube-master docker run --privileged --rm -v /lib/modules:/lib/modules mirantis/virtlet kvm-ok; then
    return 1
  fi
}

function demo::start-virtlet {
  local jq_filter='.items[0].spec.template.spec.containers[0].env|=.+[{"name": "VIRTLET_DOWNLOAD_PROTOCOL","value":"http"}]'
  if demo::kvm-ok; then
    demo::step "Deploying Virtlet DaemonSet with KVM support"
  else
    demo::step "Deploying Virtlet DaemonSet *without* KVM support"
    jq_filter="${jq_filter}"'|.items[0].spec.template.spec.containers[0].env|=.+[{"name": "VIRTLET_DISABLE_KVM","value":"y"}]'
  fi
  "${kubectl}" convert -f "${BASE_LOCATION}/deploy/virtlet-ds.yaml" --local -o json |
      docker exec -i kube-master jq "${jq_filter}" |
      "${kubectl}" create -f -
  demo::wait-for "Virtlet DaemonSet" demo::pods-ready runtime=virtlet
}

function demo::start-nginx {
  "${kubectl}" run nginx --image=nginx --expose --port 80
}

function demo::start-image-server {
  demo::step "Starting Image Server"
  "${kubectl}" create -f "${BASE_LOCATION}/examples/image-server.yaml" -f "${BASE_LOCATION}/examples/image-service.yaml"
  demo::wait-for "Image Service" demo::service-ready image-service
}

function demo::start-vm {
  demo::step "Starting sample CirrOS VM"
  "${kubectl}" create -f "${BASE_LOCATION}/examples/cirros-vm.yaml"
  demo::wait-for "CirrOS VM" demo::vm-ready cirros-vm
  if [[ ! "${NO_VM_CONSOLE:-}" ]]; then
    demo::step "Establishing ssh connection to the VM. Use Ctrl-D to disconnect"
    demo::ssh
  fi
}

if [[ ${1:-} = "--help" || ${1:-} = "-h" ]]; then
  cat <<EOF >&2
Usage: ./demo.sh

This script runs a simple demo of Virtlet[1] using kubeadm-dind-cluster[2]
ssh connection will be established after Virtlet setup is complete, Ctrl-D
can be used to disconnect from it.
Use 'curl http://nginx.default.svc.cluster.local' from VM console to test
cluster networking.

To clean up the cluster, use './dind-cluster-v1.6.sh clean'
[1] https://github.com/Mirantis/virtlet
[2] https://github.com/Mirantis/kubeadm-dind-cluster
EOF
  exit 0
fi

demo::get-dind-cluster
demo::start-dind-cluster
if [[ ${INJECT_LOCAL_IMAGE:-} ]]; then
  demo::inject-local-image
fi
demo::label-node
demo::start-virtlet
demo::start-nginx
demo::start-image-server
demo::start-vm
