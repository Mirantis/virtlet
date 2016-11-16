#!/bin/bash 
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

APISERVER_HOST=${APISERVER_HOST:-apiserver}
APISERVER_PORT=${APISERVER_PORT:-8080}
APISERVER_URL="http://${APISERVER_HOST}:${APISERVER_PORT}"

cd /go/src/k8s.io/kubernetes

function e2e::step {
  local OPTS=""
  if [ "$1" = "-n" ]; then
    shift
    OPTS+="-n"
  fi
  GREEN="$1"
  shift
  if [ -t 1 ]; then
    echo >&2 -e ${OPTS} "\x1B[97m* \x1B[92m${GREEN}\x1B[39m $*"
  else
    echo >&2 ${OPTS} "* ${GREEN} $*"
  fi
}

function e2e::serve-image {
  e2e::step "Starting serving VM image"
  (cd /images && python2 -m SimpleHTTPServer 80&)
}

function e2e::setup-kubectl {
  cluster/kubectl.sh config set-cluster local --server="${APISERVER_URL}" --insecure-skip-tls-verify=true
  cluster/kubectl.sh config set-context local --cluster=local
  cluster/kubectl.sh config use-context local
}

function e2e::wait-for-apiserver {
  local ready=
  e2e::setup-kubectl
  e2e::step "Waiting for apiserver..."
  for i in {1..50}; do
    if timeout 2 cluster/kubectl.sh get nodes >&/dev/null; then
      ready=1
      break
    fi
    sleep 5
    echo >&2 -n "."
  done
  echo >&2
  if [[ ! "${ready}" ]]; then
      echo >&2 "Timed out waiting for apiserver"
      exit 1
  fi
  cluster/kubectl.sh get nodes
  e2e::step "Apiserver ready"
}

function e2e::create-vm {
  e2e::step "Creating a VM"
  ext_ip="$(ip route get 1 | awk '{print $NF;exit}')"
  cat >/virt-cirros.yaml <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: virtlet-example-cirros
spec:
  containers:
    - name: cirros
      image: ${ext_ip}/cirros
EOF
  cluster/kubectl.sh create -f /virt-cirros.yaml
}

function e2e::wait-for-pod {
  local ready=
  e2e::step "Waiting for pod to come up"
  for i in {1..30}; do
    # FIXME: XXX: should wait for Running after #79 is fixed
    if cluster/kubectl.sh get pod virtlet-example-cirros | grep -q ContainerCreating; then
      ready=1
      break
    fi
    sleep 1
    echo >&2 -n "."
  done
  echo >&2
  if [[ ! "${ready}" ]]; then
    echo >&2 "Timed out waiting for the pod"
    exit 1
  fi
  cluster/kubectl.sh get pods
  e2e::step "Pod active"
}

function e2e::wait-for-libvirt-domain {
  local ready=
  e2e::step "Waiting for libvirt domain to become running"
  for i in {1..60}; do
    if virsh -c qemu+tcp://libvirt/system list | grep -q 'cirros.*running'; then
      ready=1
      break
    fi
    sleep 1
  done
  echo >&2
  if [[ ! "${ready}" ]]; then
    echo >&2 "Timed out waiting for libvirt domain to become running"
    exit 1
  fi
  cluster/kubectl.sh get pods
  e2e::step "VM is running"
}

function e2e::chat-with-vm {
  expect -c '
    set timeout 60
    spawn virsh -c qemu+tcp://libvirt/system console cirros
    expect {
      timeout { puts "initial message timeout"; exit 1 }
      "Escape character"
    }
    send "\r"

    expect {
      timeout { puts "login prompt timeout"; exit 1 }
      "login:"
    }
    send "cirros\r"

    expect {
      timeout { puts "password prompt timeout"; exit 1 }
      "Password: "
    }
    sleep 3
    send "cubswin:)\r"

    expect {
      timeout { puts "shell prompt timeout"; exit 1 }
      -re "\n\\$"
    }
    send "/sbin/ifconfig\r"

    expect {
      timeout { puts "shell prompt timeout"; exit 1 }
      -re "\n\\$"
    }
    send "exit\r"

    expect {
      timeout { puts "login prompt timeout"; exit 1 }
      "login:"
    }
'
}

e2e::serve-image
e2e::wait-for-apiserver
e2e::create-vm
e2e::wait-for-pod
e2e::wait-for-libvirt-domain
e2e::chat-with-vm

e2e::step "Done"
