#!/bin/bash
opts=
if [[ ${1:-} = "console" ]]; then
  # using -it with `virsh list` causes it to use \r\n as line endings,
  # which makes it less useful
  opts="-it"
fi
pod=$(kubectl get pods -l runtime=virtlet -o name|head -1|sed 's@.*/@@')
kubectl exec ${opts} "${pod}" -- virsh "$@"
