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

SCRIPT_DIR="$(cd $(dirname "$(readlinkf "${BASH_SOURCE}")"); pwd)"

source "${SCRIPT_DIR}/util.sh"

opts=
if [[ ${1:-} = "console" ]]; then
  # using -it with `virsh list` causes it to use \r\n as line endings,
  # which makes it less useful
  opts="-it"
fi
args=("$@")

if [[ ${1:-} = "poddomain" ]]; then
  if [[ $# != 2 ]]; then
    echo "poddomain command requires @podname[:namespace]" >&2
    exit 1
  fi
  get_pod_domain_id "${2}"
  exit 0
fi

for ((n=0; n < ${#args[@]}; n++)); do
  if [[ ${args[${n}]} =~ ^@ ]]; then
    args[${n}]="$(get_pod_domain_id "${args[${n}]}")"
  fi
done

pod=$(kubectl get pods -n kube-system -l runtime=virtlet -o name|head -1|sed 's@.*/@@')
kubectl exec ${opts} -n kube-system "${pod}" -c virtlet -- virsh ${args[@]+"${args[@]}"}
