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

script_dir="$(cd $(dirname "$(readlinkf "${BASH_SOURCE}")"); pwd)"

if [[ ! ( ${1:-} =~ ([^@]*@)?([^.]+)(\.(.*))? ) ]]; then
  echo "Usage: $0 [user@]pod[.namespace]" >&2
  exit 1
fi

# user_prefix includes @
user_prefix="${BASH_REMATCH[1]}"
pod_name="${BASH_REMATCH[2]}"
namespace="${BASH_REMATCH[4]}"

shift

namespace_opts=""
if [[ ${namespace} ]]; then
  namespace_opts="-o ${namespace}"
fi

read pod_ip node_name pod_namespace <<<$(kubectl get pod ${namespace_opts} "${pod_name}" -o jsonpath="{.status.podIP} {.spec.nodeName} {.metadata.namespace}")

virtlet_pod_name="$(kubectl get pod -n kube-system -l runtime=virtlet -o jsonpath="{.items[?(.spec.nodeName==\"${node_name}\")]..metadata.name}")"
if [[ ! ${virtlet_pod_name} ]]; then
  echo "Unable to locate virtlet pod on node '${node_name}'" >&2
  exit 1
fi

if [[ -z "${KEYFILE:-}" ]]; then
  KEYFILE="${script_dir}/vmkey"
  chmod 600 "${KEYFILE}"
fi

ssh -oProxyCommand="kubectl exec -i -n kube-system ${virtlet_pod_name} -c virtlet -- nc -q0 ${pod_ip} 22" \
    -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no \
    -i "${KEYFILE}" \
    "${user_prefix}${pod_name}.${pod_namespace}" "$@"
