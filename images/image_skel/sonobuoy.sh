#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

/usr/local/bin/virtletctl diag dump --json >"${RESULTS_DIR}/virtlet"
echo -n "${RESULTS_DIR}/virtlet" >"${RESULTS_DIR}/done"
