#!/bin/bash
# Copyright 2017 Mirantis
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

SCRIPT_DIR="${1}"
# Get default gateway ip from kube-master node deployed by kubeadm-dind tool
MON_IP=$(docker exec kube-master route | grep default | awk '{print $2}')
CEPH_PUBLIC_NETWORK=${MON_IP}/16
container_name="ceph_cluster"

docker rm -fv ${container_name} >&/dev/null || true

docker run -d --net=host -e MON_IP=${MON_IP} -e CEPH_PUBLIC_NETWORK=${CEPH_PUBLIC_NETWORK} --name ${container_name} ceph/demo

# Check cluster is running
set +e
ntries=20
echo -e -n "\tWaiting for ceph cluster..."
while ! docker exec ${container_name} ceph -s 2> /dev/null 1> /dev/null; do
  if [ $ntries -eq 0 ]; then
    echo "Failed to get ceph cluster status. Cluster is not running."
    exit 1
  fi
  sleep 2
  ((ntries--))
  echo -n "."
done
echo "Cluster started!"
set -e

# Adjust ceph configs
docker exec ${container_name} /bin/bash -c 'echo -e "rbd default features = 1\nrbd default format = 2" >> /etc/ceph/ceph.conf'

# Add rbd pool and volume
docker exec ${container_name} ceph osd pool create libvirt-pool 8 8
docker exec ceph_cluster /bin/bash -c "apt-get update && apt-get install -y qemu-utils"
docker exec ${container_name} qemu-img create -f rbd rbd:libvirt-pool/rbd-test-image 10M 
docker exec ${container_name} qemu-img create -f rbd rbd:libvirt-pool/rbd-test-image-pv 10M

# Add user for virtlet
docker exec ${container_name} ceph auth get-or-create client.libvirt
docker exec ceph_cluster ceph auth caps client.libvirt mon "allow *" osd "allow *" msd "allow *"
SECRET="$(docker exec ${container_name} ceph auth get-key client.libvirt)"

# Put secret into definition
sed "s^@MON_IP@^${MON_IP}^g;s^@SECRET@^${SECRET}^g" \
    "${SCRIPT_DIR}/../../examples/cirros-vm-rbd-volume.yaml.tmpl" \
    > "${SCRIPT_DIR}/cirros-vm-rbd-volume.yaml"
sed "s^@MON_IP@^${MON_IP}^g;s^@SECRET@^${SECRET}^g" \
    "${SCRIPT_DIR}/../../examples/cirros-vm-rbd-pv-volume.yaml.tmpl" \
    > "${SCRIPT_DIR}/cirros-vm-rbd-pv-volume.yaml"
