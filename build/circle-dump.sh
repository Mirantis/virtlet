#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

DIND_SCRIPT="${DIND_SCRIPT:-$HOME/dind-cluster-v1.14.sh}"
circle_token_file="$HOME/.circle-token"

job_num="${1:-}"
if [[ ! ${job_num} ]]; then
  echo >&2 "Usage: ${0} job_number"
  echo >&2
  echo >&2 "You need to have kubeadm-dind-cluster script pointed"
  echo >&2 "to by DIND_SCRIPT environment variable"
  echo >&2 "(defaults to ${DIND_SCRIPT})."
  echo >&2 "Besides, you need to specify your CircleCI token via"
  echo >&2 "CIRCLE_TOKEN environment variable or have it stored"
  echo >&2 "using base64 encoding in ${circle_token_file}."
  exit 1
fi

if [[ ! ${CIRCLE_TOKEN:-} && ! -e ${circle_token_file} ]]; then
  echo >&2 "You need to specify CIRCLE_TOKEN or store base64-encoded CircleCI token in ${circle_token_file}"
  exit 1
fi

CIRCLE_TOKEN="${CIRCLE_TOKEN:-"$(base64 --decode "${circle_token_file}")"}"

if [[ ! -e "${DIND_SCRIPT}" ]]; then
  echo >&2 "Please specify the path to kubeadm-dind-cluster script with DIND_SCRIPT"
  exit 1
fi

base_url="https://circleci.com/api/v1.1/project/github/Mirantis/virtlet"

rm -rf virtlet-circle-dump
mkdir virtlet-circle-dump
cd virtlet-circle-dump

url="$(curl -sSL -u "${CIRCLE_TOKEN}:" "${base_url}/${job_num}/artifacts" |
            jq -r '.[]|select(.path=="tmp/cluster_state/kdc-dump.gz")|.url')"
echo >&2 "Getting cluster dump from ${url}"
curl -sSL "${url}" | gunzip | ~/dind-cluster-v1.14.sh split-dump

url="$(curl -sSL -u "${CIRCLE_TOKEN}:" "${base_url}/${job_num}/artifacts" |
            jq -r '.[]|select(.path=="tmp/cluster_state/virtlet-dump.json.gz")|.url')"
echo >&2 "Getting virtlet dump from ${url}"
curl -sSL "${url}" | gunzip | virtletctl diag unpack virtlet/
