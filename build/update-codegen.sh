#!/bin/bash
# TODO: move this to cmd.sh once we upgrade to Go 1.10

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(dirname ${BASH_SOURCE})/.."
CODEGEN_PKG="${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ${GOPATH}/src/k8s.io/code-generator)}"

vendor/k8s.io/code-generator/generate-groups.sh all \
  github.com/Mirantis/virtlet/pkg/client github.com/Mirantis/virtlet/pkg/api \
  virtlet.k8s:v1 \
  --go-header-file "${SCRIPT_ROOT}/build/custom-boilerplate.go.txt"

# fix import url case issues
find "${SCRIPT_ROOT}/pkg/client" -name '*.go' -exec sed -i 's@github\.com/mirantis/virtlet@github\.com/Mirantis/virtlet@g' '{}' \;
