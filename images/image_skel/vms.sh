#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

echo "$$ $(cut -d' ' -f22 /proc/$$/stat)" >/var/lib/virtlet/vms.procfile
sleep Infinity
