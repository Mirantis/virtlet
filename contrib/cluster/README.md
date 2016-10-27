##Description
Scripts to deploy local k8s cluster with virtlet runtime based on lxc containers with nested docker containers multi-node cluster.

##Assumptions/Prerequisits

 * assumed GOPATH is set
 * lxc is installed
 * check the LXCFS is not installed, kubelet is not assumed to be able to parse /proc/self/mountinfo correctly in this case
 * use linux kernel >= 4.2.0
 * stop apparmor and unload profiles:
```sh
/etc/init.d/apparmor stop
/etc/init.d/apparmor teardown
```sh

 * set up default config for all lxc containers to be able to start nested docker and run VMs
 ```sh
# cat /etc/lxc/default.conf
lxc.network.type = veth
lxc.network.link = lxcbr0
lxc.network.flags = up
lxc.network.hwaddr = 00:16:3e:xx:xx:xx
```sh

# Permit access to /dev/loop*
lxc.cgroup.devices.allow = b 7:* rwm

# Setup access to /dev/net/tun and /dev/kvm
lxc.mount.entry = /dev/net/tun dev/net/tun none bind,create=file 0 0
lxc.mount.entry = /dev/kvm dev/kvm none bind,create=file 0 0

lxc.rootfs.backend = dir
lxc.mount.auto = proc:rw sys:rw cgroup-full:rw
lxc.aa_profile = unconfined
lxc.se_context = unconfined_u:unconfined_r:lxc_t:s0-s0:c0.c1023
lxc.cap.drop =

 ```sh
 * Use K8S_OUTPUT_DIR env variable to set up dir with built docker images and binaries, by default it is set to
$GOPATH/src/k8s.io/kubernetes/_output/release-stage/server/linux-amd64/kubernetes/server/bin
 * Virtlet and libvirt images are taken from host docker


##Usage

Script is assumed to start from $GOPATH/src/github.com/Mirantis/virtlet/

* Start cluster, 
```sh
./lxc_utils.sh up
```sh

* Remove/Stop cluster
```sh
./lxc_utils.sh down
```sh

* If cluster was deployed successfully api-server ip and port will be printed:
```sh
SUCCESS
K8s cluster started!

Kubernetes master is running at http://192.168.122.7:8080


# _output/bin/kubectl -s=192.168.122.7:8080 get nodes
NAME                   STATUS    AGE
virtlet-auto-master    Ready     4m
virtlet-auto-minion1   Ready     1m
virtlet-auto-minion2   Ready     15s


```sh

As you see, by default, cluster consists of one node with mixed role (master+minion) 
and two minions, to change the number of minions set $NUM_NODES variable.

## Practice:
```sh
##After cluster is deployed:

# lxc-ls --fancy
NAME                 STATE   AUTOSTART GROUPS IPV4                                    IPV6
VIRTLET-AUTO-master  RUNNING 0         -      172.17.0.1, 192.168.122.7               -
VIRTLET-AUTO-minion1 RUNNING 0         -      172.17.0.1, 172.18.0.1, 192.168.122.215 -
VIRTLET-AUTO-minion2 RUNNING 0         -      172.17.0.1, 172.18.0.1, 192.168.122.217 -
base_k8s_virtlet     STOPPED 0         -      -                                       -

##Where base_k8s_virtlet is a reference container based on ubuntu trusty beased on which nodes containers are cloned to speed up deployment.
##After reference container was created it is used until woudn't be removed manually. Only folders with 
configs and binaries for k8s cluster are updated.

## K8s master PODs list:
# _output/bin/kubectl -s=192.168.122.7:8080 get pods
NAME                                          READY     STATUS              RESTARTS   AGE
etcd-server-virtlet-auto-master               1/1       Running             0          5m
kube-apiserver-virtlet-auto-master            1/1       Running             1          5m
kube-controller-manager-virtlet-auto-master   1/1       Running             0          5m
kube-scheduler-virtlet-auto-master            1/1       Running             0          3m


##So let's create VM-pod:

# cat /root/projects/k8s_manifests/simplePod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-vm-fedora
spec:
  containers:
    - name: "test-vm-fedora"
      image: 192.168.122.1/fedora
#
#
# _output/bin/kubectl -s=192.168.122.7:8080 create -f /root/projects/k8s_manifests/simplePod.yaml
# 
# _output/bin/kubectl -s=192.168.122.7:8080 get pods/test-vm-fedora
NAME             READY     STATUS              RESTARTS   AGE
test-vm-fedora   0/1       ContainerCreating   0          1m

## Overall info regarding VM-pod:
# _output/bin/kubectl -s=192.168.122.7:8080 describe pods/test-vm-fedora
Name:           test-vm-fedora
Namespace:      default
Node:           virtlet-auto-minion2/192.168.122.217
Start Time:     Mon, 14 Nov 2016 21:46:45 +0000
Labels:         <none>
Status:         Pending
IP:
Controllers:    <none>
Containers:
  test-vm-fedora:
    Container ID:
    Image:                      192.168.122.1/fedora
    Image ID:
    Port:
    State:                      Waiting
      Reason:                   ContainerCreating
    Ready:                      False
    Restart Count:              0
    Volume Mounts:              <none>
    Environment Variables:      <none>
Conditions:
  Type          Status
  Initialized   True
  Ready         False
  PodScheduled  True
No volumes.
QoS Class:      BestEffort
Tolerations:    <none>
Events:
  FirstSeen     LastSeen        Count   From                            SubObjectPath                   Type            Reason                  Message
  ---------     --------        -----   ----                            -------------                   --------        ------                  -------
  1m            1m              1       {default-scheduler }                                            Normal          Scheduled               Successfully assigned test-vm-fedora to virtlet-auto-minion2
  1m            1m              1       {kubelet virtlet-auto-minion2}                                  Normal          SandboxReceived         Pod sandbox received, it will be created.
  1m            1m              2       {kubelet virtlet-auto-minion2}  spec.containers{test-vm-fedora} Normal          Pulling                 pulling image "192.168.122.1/fedora"
  1m            58s             8       {kubelet virtlet-auto-minion2}                                  Warning         MissingClusterDNS       kubelet does not have ClusterDNS IP configured and cannot create Pod using "ClusterFirst" policy. Falling back to DNSDefault policy.
  1m            58s             2       {kubelet virtlet-auto-minion2}  spec.containers{test-vm-fedora} Normal          Pulled                  Successfully pulled image "192.168.122.1/fedora"
  1m            58s             2       {kubelet virtlet-auto-minion2}  spec.containers{test-vm-fedora} Normal          Created                 Created container with id ee0ebfbd-a6b4-49c9-593e-95dd3e1daea4
  1m            58s             2       {kubelet virtlet-auto-minion2}  spec.containers{test-vm-fedora} Normal          Started                 Started container with id ee0ebfbd-a6b4-49c9-593e-95dd3e1daea4

##Let's check inside containers:

# lxc-attach -n VIRTLET-AUTO-minion2
root@VIRTLET-AUTO-minion2:/# docker ps
CONTAINER ID        IMAGE                   COMMAND                  CREATED             STATUS              PORTS               NAMES
7f957eef0535        dockercompose_virtlet   "/usr/local/bin/virtl"   4 minutes ago       Up 3 minutes                            dockercompose_virtlet_1
ac54d9653c54        dockercompose_libvirt   "/start.sh"              4 minutes ago       Up 3 minutes                            dockercompose_libvirt_1
root@VIRTLET-AUTO-minion2:/# docker exec ac54d9653c54 virsh list
 Id    Name                           State
----------------------------------------------------
 1     test-vm-fedora                 running

```sh
