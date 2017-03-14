#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

dind_script="dind-cluster-v1.5.sh"
kubectl="${HOME}/.kubeadm-dind-cluster/kubectl"
base_url="https://raw.githubusercontent.com/Mirantis/virtlet/ivan4th/kubeadm-dind-cluster-deployment/"

function demo::step {
  local OPTS=""
  if [ "$1" = "-n" ]; then
    shift
    OPTS+="-n"
  fi
  GREEN="$1"
  shift
  if [ -t 2 ] ; then
    echo -e ${OPTS} "\x1B[97m* \x1B[92m${GREEN}\x1B[39m $*" 1>&2
  else
    echo ${OPTS} "* ${GREEN} $*" 1>&2
  fi
}

function demo::get-dind-cluster {
  if [[ -f ${dind_script} ]]; then
    return 0
  fi
  demo::step "Will download dind-cluster-v1.5.sh into current directory. Press Enter to continue or Ctrl-C to stop."
  read
  wget "https://raw.githubusercontent.com/Mirantis/kubeadm-dind-cluster/master/fixed/${dind_script}"
  chmod +x "${dind_script}"
}

function demo::start-dind-cluster {
  demo::step "Will now clear any kubeadm-dind-cluster data on the current Docker. Press Enter to continue or Ctrl-C to stop."
  read
  "./${dind_script}" clean
  "./${dind_script}" up
}

function demo::label-node {
  demo::step "Applying label to kube-node-1:" "extraRuntime=virtlet"
  "${kubectl}" label node kube-node-1 extraRuntime=virtlet
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
  if ! "${kubectl}" describe service "${name}"|grep -q '^Endpoints'; then
    return 1
  fi
}

function demo::wait-for {
  local action="${1}"
  local what="$2"
  local title="$3"
  demo::step "Waiting for:" "${title}"
  while ! "${action}" "${what}"; do
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
  "${kubectl}" exec ${opts} -n kube-system "${virtlet_pod}" -- virsh "$@"
}

function demo::vm-ready {
  local name="$1"
  # note that the following is not a bulletproof check
  if ! demo::virsh list --name | grep -q "${name}\$"; then
    return 1
  fi
}

function demo::start-virtlet {
  demo::step "Deploying Virtlet DaemonSet"
  "${kubectl}" create -f "${base_url}/deploy/virtlet-ds.yaml"
  demo::wait-for demo::pods-ready runtime=virtlet "Virtlet DaemonSet"
}

function demo::start-nginx {
  "${kubectl}" run nginx --image=nginx --expose --port 80
}

function demo::start-image-server {
  demo::step "Starting Image Server"
  "${kubectl}" create -f "${base_url}/examples/image-server.yaml" -f "${base_url}/examples/image-service.yaml"
  demo::wait-for demo::service-ready image-service "Image Service"
}

function demo::start-vm {
  demo::step "Starting sample CirrOS VM"
  "${kubectl}" create -f "${base_url}/examples/cirros-vm.yaml"
  demo::wait-for demo::vm-ready cirros-vm "CirrOS VM"
  demo::step "Entering the VM, press Enter if you don't see the prompt or OS boot messages"
  demo::virsh console $(demo::virsh list --name)
}

demo::get-dind-cluster
demo::start-dind-cluster
demo::label-node
demo::start-virtlet
demo::start-nginx
demo::start-image-server
demo::start-vm
