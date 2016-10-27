#!/bin/bash

set -o errexit
set -o nounset

LXC_PATH="/var/lib/lxc/"

K8S_OUTPUT_DIR=${K8S_OUTPUT_DIR:-$GOPATH/src/k8s.io/kubernetes/_output/release-stage/server/linux-amd64/kubernetes/server/bin}

NUM_NODES=${NUM_NODES:-2}
VIRTLET_IMAGE_NAME=${VIRTLET_IMAGE_NAME:-dockercompose_virtlet}
LIBVIRT_IMAGE_NAME=${LIBVIRT_IMAGE_NAME:-dockercompose_libvirt}

NET_CALICO=${NET_CALICO:-false}

BASE_IMAGE_WORK_DIR=${BASE_IMAGE_WORK_DIR:-/home/ubuntu/virtlet}

BASE_CONTAINER_NAME=${BASE_CONTAINER_NAME:-base_k8s_virtlet}
VIRTLET_SOCK=${VIRTLET_SOCK:-/run/virtlet.sock}

MASTER_IP=""
KUBELET_OPTS=""
PROXY_OPTS=""
CONTAINER_PREFIX_NAME="VIRTLET-AUTO-"

declare -r RED="\033[0;31m"
declare -r GREEN="\033[0;32m"
declare -r YELLOW="\033[0;33m"

function echo_green {
  echo -e "${GREEN}$1"; tput sgr0
}

function echo_red {
  echo -e "${RED}$1"; tput sgr0
}

function echo_yellow {
  echo -e "${YELLOW}$1"; tput sgr0
}

function setup_master() {
    mac=$(lxc-info -n $1 -c lxc.network.0.hwaddr | sed 's/.* = //g')
    MASTER_IP=$(lxc-attach -n $1 -- ip a | grep -A1 $mac | grep inet | awk '{print $2}' | sed 's/\/.*//g')
    lxc-attach -n $1 -- touch ${BASE_IMAGE_WORK_DIR}/start_master.sh

# Add k8s images to docker daemon
# TODO: Get rid of setting latest tag  for images by setting up correct tag in pod's definition
# TODO: Get rid of sleeps
cmd="#!/bin/bash
docker load -i ${BASE_IMAGE_WORK_DIR}/kube-apiserver.tar
image_id=\\\$(docker images | grep kube-apiserver | awk '{print \\\$3; exit}')
docker tag \\\$image_id gcr.io/google_containers/kube-apiserver:latest
docker load -i ${BASE_IMAGE_WORK_DIR}/kube-controller-manager.tar
image_id=\\\$(docker images | grep kube-controller-manager | awk '{print \\\$3; exit}')
docker tag \\\$image_id gcr.io/google_containers/kube-controller-manager:latest
docker load -i ${BASE_IMAGE_WORK_DIR}/kube-scheduler.tar
image_id=\\\$(docker images | grep kube-scheduler | awk '{print \\\$3; exit}')
docker tag \\\$image_id gcr.io/google_containers/kube-scheduler:latest
docker load -i ${BASE_IMAGE_WORK_DIR}/kube-proxy.tar
image_id=\\\$(docker images | grep kube-proxy | awk '{print \\\$3; exit}')
docker tag \\\$image_id gcr.io/google_containers/kube-proxy:latest
${BASE_IMAGE_WORK_DIR}/kubelet --config=${BASE_IMAGE_WORK_DIR}/k8s_master_pods --api-servers=http://localhost:8080 --v=5 > /var/log/kubelet.log 2>&1 &
"

    lxc-attach -n $1  -- sudo /bin/bash -c "echo \"$cmd\" > ${BASE_IMAGE_WORK_DIR}/start_master.sh"
    lxc-attach -n $1  -- sudo chmod +x ${BASE_IMAGE_WORK_DIR}/start_master.sh
    lxc-attach -n $1  -- sed -i -e '$i'"${BASE_IMAGE_WORK_DIR}/start_master.sh"'\' /etc/rc.local
    lxc-attach -n $1  -- sudo bash -x ${BASE_IMAGE_WORK_DIR}/start_master.sh

    lxc-stop -n $1
    lxc-start -n $1

    echo -e -n "\tWaiting for master components to start..."
    while true; do
       local running_count=$(${K8S_OUTPUT_DIR}/kubectl -s=http://${MASTER_IP}:8080 get pods --no-headers 2>/dev/null | grep "Running" | wc -l)
       # We expect to have 4 running pods.
       if [ "$running_count" -ge 4 ]; then
          break
       fi
       echo -n "."
       sleep 1
    done
    echo ""
    echo_green "SUCCESS"
    echo_green "K8s master started!"
    echo ""
    ${K8S_OUTPUT_DIR}/kubectl -s http://${MASTER_IP}:8080 clusterinfo
}

function setup_node() {
    lxc-attach -n $1 -- touch ${BASE_IMAGE_WORK_DIR}/start_node.sh

cmd="#!/bin/bash

docker load -i ${BASE_IMAGE_WORK_DIR}/${VIRTLET_IMAGE_NAME}.tar
docker load -i ${BASE_IMAGE_WORK_DIR}/${LIBVIRT_IMAGE_NAME}.tar

cd ${BASE_IMAGE_WORK_DIR}/docker-compose
docker-compose up -d
sleep 10

${BASE_IMAGE_WORK_DIR}/kubelet ${KUBELET_OPTS} > /var/log/kubelet.log 2>&1 &
${BASE_IMAGE_WORK_DIR}/kube-proxy ${PROXY_OPTS} > /var/log/kube-proxy.log 2>&1 &
"
    lxc-attach -n $1  --  sudo /bin/bash -c "echo \"$cmd\" > ${BASE_IMAGE_WORK_DIR}/start_node.sh"
    lxc-attach -n $1  -- sudo chmod +x ${BASE_IMAGE_WORK_DIR}/start_node.sh
    lxc-attach -n $1  -- sed -i -e '$i'"${BASE_IMAGE_WORK_DIR}/start_node.sh"'\' /etc/rc.local
    lxc-attach -n $1  -- sudo bash -x ${BASE_IMAGE_WORK_DIR}/start_node.sh
}

function create-update-base-container() {
    if [ ! -d ${K8S_OUTPUT_DIR} ]; then
       echo_red "Coudn't find directory with built k8s binaries and images: ${K8S_OUTPUT_DIR}"
       echo_red "Probably you haven't built them, please go to $GOPATHi/src/k8s/kubernetes and run 'make realese'"
    fi
    if ! lxc-ls | grep -q ${BASE_CONTAINER_NAME}; then
       lxc-create --template ubuntu --name ${BASE_CONTAINER_NAME} --logfile ./log_for_base_lxc -- --release trusty
       lxc-start --name ${BASE_CONTAINER_NAME}
       wait_for_ip ${BASE_CONTAINER_NAME}
       lxc-wait --name ${BASE_CONTAINER_NAME} --state RUNNING
       # Setup base container
       <./setup_base_container.sh lxc-attach -n ${BASE_CONTAINER_NAME} -- sudo sh
       lxc-stop --name ${BASE_CONTAINER_NAME}
    else
       if [ "`lxc-ls --fancy | grep ${BASE_CONTAINER_NAME} | awk '{print $2}'`" == "RUNNING" ]; then
          lxc-stop --name ${BASE_CONTAINER_NAME}
       fi
    fi

    local container_dir="${LXC_PATH}${BASE_CONTAINER_NAME}/rootfs/home/ubuntu/virtlet"

    if [ -d ${container_dir} ]; then
       echo_green "Found base container virtlet dir: ${container_dir}. Removing..."
       rm -rf ${container_dir}
    fi

    # Update directory content
    echo_green "Update pods yamls"
    update-pods-yamls

    echo_green "Create new container virtlet dir and fill it with k8s and virtlet images"
    mkdir ${container_dir}

    cp -f ${K8S_OUTPUT_DIR}/kube-*.tar ${container_dir}
    cp -f ${K8S_OUTPUT_DIR}/kubelet ${container_dir}/kubelet
    cp -f ${K8S_OUTPUT_DIR}/kube-proxy ${container_dir}/kube-proxy
    cp -rf ./k8s_master_pods ${container_dir}

    # Prepare virtlet images and docker-cimpose yaml
    copy-virtlet-images
    cp -f "${VIRTLET_IMAGE_NAME}.tar" ${container_dir}
    cp -f "${LIBVIRT_IMAGE_NAME}.tar" ${container_dir}
    cp -rf ../docker-compose ${container_dir}
    sed -i 'N;s/virtlet:\n.*build:.*/virtlet:\n    image: dockercompose_virtlet/' ${container_dir}/docker-compose/docker-compose.yml
    sed -i 'N;s/libvirt:\n.*build:.*/libvirt:\n    image: dockercompose_libvirt/' ${container_dir}/docker-compose/docker-compose.yml
}

function substitute-env-in-file() {
    IFS='%'; while read line; do eval echo \"$line\"; done < $1 > $2
}

function update-pods-yamls() {
    create-kube-apiserver-opts 192.168.3.0/24
    create-kube-controller-manager-opts 172.16.0.0/16 192.168.3.0/24
    create-kube-scheduler-opts

    rm -rf ./k8s_master_pods
    mkdir ./k8s_master_pods

    substitute-env-in-file ./master/static_pods/kube-apiserver.yaml ./k8s_master_pods/kube-apiserver.yaml
    substitute-env-in-file ./master/static_pods/kube-controller-manager.yaml ./k8s_master_pods/kube-controller-manager.yaml
    substitute-env-in-file ./master/static_pods/kube-scheduler.yaml ./k8s_master_pods/kube-scheduler.yaml
    cp ./master/static_pods/etcd.yaml ./k8s_master_pods/etcd.yaml
}

function copy-virtlet-images() {
    rm -f "./${VIRTLET_IMAGE_NAME}.tar"
    rm -f "./${LIBVIRT_IMAGE_NAME}.tar"

    # Save the docker image as a tar file
    docker save -o "${VIRTLET_IMAGE_NAME}.tar" ${VIRTLET_IMAGE_NAME}
    docker save -o "${LIBVIRT_IMAGE_NAME}.tar" ${LIBVIRT_IMAGE_NAME}
}


# $1: CIDR block for service addresses.
function create-kube-apiserver-opts() {
    export APISERVER_OPTS="\
     --insecure-bind-address=0.0.0.0\
     --insecure-port=8080\
     --etcd-servers=http://127.0.0.1:2379\
     --logtostderr=true\
     --service-cluster-ip-range=${1}\
     --admission-control=NamespaceLifecycle,LimitRanger,DefaultStorageClass,ResourceQuota\
     --allow-privileged=true\
     --v=5"
}

# $1: CIDR block for pods addresses.
# $2: CIDR block for service addresses.
function create-kube-controller-manager-opts() {
    export CONTROLLER_OPTS="\
     --master=127.0.0.1:8080\
     --cluster-cidr=${1}\
     --service-cluster-ip-range=${2}\
     --v=5\
     --logtostderr=true"
}

# Create ~/kube/default/kube-scheduler with proper contents.
function create-kube-scheduler-opts() {
    export SCHEDULER_OPTS="\
     --logtostderr=true\
     --master=127.0.0.1:8080\
     --v=5"
}

function create-kubelet-opts() {
    if [ -n "$1" ] ; then
        cni_opts=" --network-plugin=cni --network-plugin-dir=/etc/cni/net.d"
    else
        cni_opts=""
    fi
    export KUBELET_OPTS="\
     --api-servers=http://${MASTER_IP}:8080 \
     --container-runtime=remote \
     --container-runtime-endpoint=/run/virtlet.sock \
     --image-service-endpoint=/run/virtlet.sock \
     --logtostderr=true \
     --cluster-dns= \
     --cluster-domain= \
     --config= \
     --allow-privileged=true\
     --v=5\
     $cni_opts"
}

function create-kube-proxy-opts() {
    export PROXY_OPTS="\
     --master=http://${MASTER_IP}:8080 \
     --logtostderr=true \
     --conntrack-max=0 \
     --conntrack-max-per-core=0 \
     --v=5"
}

function cluster_down() {
    #Stop and remove all containers with specific prefix
    IFS=', ' read -r -a containers <<< $(lxc-ls --fancy | grep ${CONTAINER_PREFIX_NAME} | awk {'printf " %s,",$1'})    
    if [ ${#containers[@]} -gt 0 ]; then
       for cont in "${containers[@]}"
       do
          if [ "`lxc-ls --fancy | grep ${cont} | awk '{print $2}'`" == "RUNNING" ]; then
             lxc-stop --name ${cont}
          fi
          lxc-destroy -n $cont
       done
    fi
}

function wait_for_ip() {
    echo -e -n "\tWaiting for ip to set for $1..."
    while true; do
       if [ "`lxc-ls --fancy | grep $1 | awk '{print $5}'`" != "-"  ]; then
          break
       fi
       echo -n "."
       sleep 1
    done
    echo_green "DONE: Ip is set"
}

function cluster_up() {
    create-update-base-container

    lxc-copy -n ${BASE_CONTAINER_NAME} -N "${CONTAINER_PREFIX_NAME}master"
    lxc-start -n "${CONTAINER_PREFIX_NAME}master"
    lxc-wait --name "${CONTAINER_PREFIX_NAME}master" --state RUNNING
    wait_for_ip "${CONTAINER_PREFIX_NAME}master"
    setup_master "${CONTAINER_PREFIX_NAME}master"

    create-kubelet-opts ""
    create-kube-proxy-opts

    for (( i=1; i<=${NUM_NODES}; i++ ))
    do
       lxc-copy -n ${BASE_CONTAINER_NAME} -N "${CONTAINER_PREFIX_NAME}node$i"
       lxc-start -n "${CONTAINER_PREFIX_NAME}node$i"
       lxc-wait --name "${CONTAINER_PREFIX_NAME}node$i" --state RUNNING
       wait_for_ip "${CONTAINER_PREFIX_NAME}node$i"
       setup_node "${CONTAINER_PREFIX_NAME}node$i"
       lxc-stop -n "${CONTAINER_PREFIX_NAME}node$i"
       lxc-start -n "${CONTAINER_PREFIX_NAME}node$i"
    done

    echo -e -n "\tChecking nodes..."
    while true; do
       local nodes_count=$(${K8S_OUTPUT_DIR}/kubectl -s=http://${MASTER_IP}:8080 get nodes --no-headers 2>/dev/null | wc -l)
       # We expect to have ${NUM_NODES} running nodes
       if [ "$nodes_count" -gt ${NUM_NODES} ]; then
          break
       fi
       echo -n "."
       sleep 1
    done
    echo ""
    echo_green "SUCCESS"
    echo_green "K8s cluster started!"
    echo ""
    ${K8S_OUTPUT_DIR}/kubectl -s http://${MASTER_IP}:8080 clusterinfo
}

if [ $# -eq 0 ]; then
    echo_red "Got no arguments. Expect to receive command to execute: \"up\" or \"down\""
elif [ "$1" == "up" ]; then
    cluster_up
elif [ "$1" == "down" ]; then
    cluster_down
else
    echo_red "Unknown command was specified: $1. Expect to receive command to execute: \"up\" or \"down\""
fi
