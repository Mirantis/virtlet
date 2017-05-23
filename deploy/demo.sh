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
DEPLOY_LOG_CONTAINER=${DEPLOY_LOG_CONTAINER:-0} # 0 = don't deploy, 1 = deploy, 2 = inject local log image and deploy

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
    return 0
  fi
  demo::step "Will download dind-cluster-v1.6.sh into current directory"
  demo::ask-before-continuing
  wget "https://raw.githubusercontent.com/Mirantis/kubeadm-dind-cluster/master/fixed/${dind_script}"
  chmod +x "${dind_script}"
}

function demo::start-dind-cluster {
  demo::step "Will now clear any kubeadm-dind-cluster data on the current Docker"
  if [[ ! ${NONINTERACTIVE} ]]; then
    echo "VM console will be attached after Virtlet setup is complete, press Ctrl-] to detach." >&2
  fi
  echo "To clean up the cluster, use './dind-cluster-v1.6.sh clean'" >&2
  demo::ask-before-continuing
  "./${dind_script}" clean
  "./${dind_script}" up
}

function demo::inject-local-image {
  demo::step "Copying local mirantis/virtlet image into kube-node-1 container"
  docker save mirantis/virtlet | docker exec -i kube-node-1 docker load
}

function demo::inject-local-log-image {
  demo::step "Copying local mirantis/virtlet-log image into kube-node-1 container"
  docker save mirantis/virtlet-log | docker exec -i kube-node-1 docker load
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
  "${kubectl}" exec ${opts} -n kube-system "${virtlet_pod}" -- virsh "$@"
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
  # The check is done inside node-1 container because it has proper /lib/modules
  # from the docker host. Also, it'll have to use mirantis/virtlet image
  # later anyway.
  if ! docker exec kube-node-1 docker run --privileged --rm -v /lib/modules:/lib/modules mirantis/virtlet kvm-ok; then
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

function demo::start-virtlet-log {
  demo::step "Deploying Virtlet Log DaemonSet"
  "${kubectl}" create -f "${BASE_LOCATION}/deploy/virtlet-log-ds.yaml"
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
    demo::step "Entering the VM, press Enter if you don't see the prompt or OS boot messages"
    demo::virsh console $(demo::virsh list --name)
  fi
}

if [[ ${1:-} = "--help" || ${1:-} = "-h" ]]; then
  cat <<EOF >&2
Usage: ./demo.sh

This script runs a simple demo of Virtlet[1] using kubeadm-dind-cluster[2]
VM console will be attached after Virtlet setup is complete, Ctrl-]
can be used to detach from it.
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
if [[ ${DEPLOY_LOG_CONTAINER:-} == 2 ]]; then
  demo::inject-local-log-image
fi 
demo::label-node
demo::start-virtlet
if [[ ${DEPLOY_LOG_CONTAINER:-} > 0 ]]; then
  demo::start-virtlet-log  
fi
demo::start-nginx
demo::start-image-server
demo::start-vm
