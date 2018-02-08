#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

PLUGINBASEDIR="${HOME}/.kube/plugins/virt"

opts=
if [[ ${1:-} = "console" || $# == 0 ]]; then
  # using -it with `virsh list` causes it to use \r\n as line endings,
  # which makes it less useful
  opts="-it"
fi
args=("$@")

pod_output=""

function install_as_kubectl_plugin {
  mkdir -p "${PLUGINBASEDIR}"
  cp "${0}" "${PLUGINBASEDIR}/virt"
  chmod +x "${PLUGINBASEDIR}/virt"
  cat >"${PLUGINBASEDIR}/plugin.yaml" <<__EOF__
name: "virt"
shortDesc: "Interface to libvirt's virsh in Virtlet container"
command: "./virt"
__EOF__
}

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
  info=$(kubectl get pod ${namespace_opts} "${pod_name}" -o jsonpath="{.status.containerStatuses[0].containerID} {.status.containerStatuses[0].name} {.spec.nodeName}")
  if [[ $? > 0 ]]; then
    exit 1
  fi

  read container_id container_name node_name <<<"${info}"
  container_id=$(echo ${container_id} | sed 's/.*__//' | cut -c1-13)

  new_pod_output=jsonpath="{.items[?(.spec.nodeName==\"${node_name}\")]..metadata.name}"
  if [[ "${pod_output}" && "${new_pod_output}" != "${pod_output}" ]]; then
    echo "Cannot refer to several domains hosted on different nodes" >&2
    exit 1
  fi
  pod_output=${new_pod_output}
  domain="virtlet-${container_id}-${container_name}"
}

if [[ ${1:-} = "install" ]]; then
  install_as_kubectl_plugin
  exit 0
fi

if [[ ${1:-} = "poddomain" ]]; then
  if [[ $# != 2 ]]; then
    echo "poddomain command requires @podname[:namespace]" >&2
    exit 1
  fi
  get_pod_domain_id "${2}"
  echo "${domain}"
  exit 0
fi

for ((n=0; n < ${#args[@]}; n++)); do
  if [[ ${args[${n}]} =~ ^@ ]]; then
    get_pod_domain_id "${args[${n}]}"
    args[${n}]="${domain}"
  fi
done

pods=($(kubectl get pods -n kube-system -l runtime=virtlet -o "${pod_output:-name}" | sed 's@.*/@@'))

if [[ ! ${pods+x} ]]; then
  if [[ ${pod_output} ]]; then
    echo "No virtlet pod for specified domain can be found" >&2
  else
    echo "No virtlet pod found " >&2
  fi
  exit 1
elif [[ ${#pods[*]} > 1 ]]; then
  printf "WARNING: More than one virtlet pod found. Executing command in all instances\n\n"
fi

for pod in ${pods[*]}; do
  if [[ ${#pods[*]} > 1 ]]; then
    printf "${pod} ($(kubectl get pod -nkube-system "${pod}" -o jsonpath="{.spec.nodeName}")):\n\n"
  fi
  kubectl exec ${opts} -n kube-system "${pod}" -c libvirt -- virsh ${args[@]+"${args[@]}"}
done
