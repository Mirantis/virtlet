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

function e2e::wait {
  local attempts="$1"
  local delay="$2"
  local cmd="$3"
  local what="$4"
  local ready=
  e2e::step "Waiting for ${what}..."
  for (( i = 0; i < attempts; i++ )); do
    if eval "$cmd"; then
      ready=1
      break
    fi
    sleep ${delay}
    echo >&2 -n "."
  done
  echo >&2
  if [[ ! "${ready}" ]]; then
      echo >&2 "Timed out waiting for ${what}"
      exit 1
  fi
  e2e::step "Done waiting for ${what}"
}

function e2e::wait-for-apiserver {
  e2e::wait 100 5 "timeout 2 bash -c 'cluster/kubectl.sh get nodes|grep -q Ready' >&/dev/null" "apiserver"
  e2e::wait 30 1 "cluster/kubectl.sh get sa | grep -q '^default'" "default service account"
  cluster/kubectl.sh get nodes
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
  e2e::wait 100 3 "cluster/kubectl.sh get pod virtlet-example-cirros | grep -q Running" \
            "pod to come up"
  cluster/kubectl.sh get pods
}

function e2e::wait-for-libvirt-domain {
  # TODO: don't use tcp
  e2e::wait 100 3 "virsh -c qemu+tcp://virtlet/system list | grep -q 'cirros.*running'" \
            "libvirt domain to become running"
  virsh -c qemu+tcp://virtlet/system list
}

function e2e::chat-with-vm {
  expect -c '
    set timeout 600
    spawn virsh -c qemu+tcp://virtlet/system console cirros
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

    set timeout 20
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

e2e::setup-kubectl
e2e::serve-image
e2e::wait-for-apiserver
e2e::create-vm
e2e::wait-for-pod
e2e::wait-for-libvirt-domain
e2e::chat-with-vm

e2e::step "Done"
