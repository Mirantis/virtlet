#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

function get_pod_domain_id {
  # @pod[:namespace]
  if [[ ! ( ${1} =~ ^@([^:]+)(:(.*))?$ ) ]]; then
    echo "bad podspec ${1}" >&2
    exit 1
  fi

  local pod_name="${BASH_REMATCH[1]}"
  local namespace="${BASH_REMATCH[3]}"
  local namespace_opts=""
  if [[ ${namespace} ]]; then
      namespace_opts="-n ${namespace}"
  fi
  kubectl get pod ${namespace_opts} "${pod_name}" \
          -o 'jsonpath={.status.containerStatuses[0].containerID}-{.status.containerStatuses[0].name}'|sed 's/.*__//'
}
