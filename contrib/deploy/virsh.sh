#!/bin/bash
# FIXME: trying to do `kubectl exec` in virtlet container may currently fail with unhelpful error message ("Error from server")
opts=
if [[ ${1:-} = "console" ]]; then
  # using -it with `virsh list` causes it to use \r\n as line endings,
  # which makes it less useful
  opts="-it"
fi
docker exec ${opts} kube-node-1 docker exec ${opts} $(docker exec kube-node-1 docker ps|grep mirantis/virtlet|sed 's/.* //') virsh "$@"
