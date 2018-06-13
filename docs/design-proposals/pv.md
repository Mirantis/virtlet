# Making use of Local PVs and Block Volume mode

Virtlet uses custom flexvolume driver to handle raw block devices and
Ceph volumes right now. This makes VM pods less consistent with
"plain" Kubernetes pods. Another problem is that we may want to support
persistent rootfs in future. As there's now Local Persistent Volume
support (beta as of 1.10) and Block Volume support (alpha as of 1.10)
in Kubernetes, we may use these features in Virtlet to avoid the
flexvolume hacks and gain persistent rootfs support.

This document contains the results of the research and will be turned
into a more detailed proposal later if we decide to make use of
the block PVs.

The research is based on
[this Kubernetes blog post](https://kubernetes.io/blog/2018/04/13/local-persistent-volumes-beta/#enabling-smarter-scheduling-and-volume-binding)
and
[the raw block volume description](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#raw-block-volume-support)
from Kubernetes documentation.

First, I'll describe how the block PVs can be used in Virtlet, and
then I'll give a detailed description of how the experiments were
conducted.

## Using block PVs in Virtlet

As it turns out, the non-local block PVs aren't different from local
block PVs from the CRI point of view. They're configured using
`volumeDevices` section of the container spec in the pod and `volumes`
section of the pod spec, and passed as `devices` section in the
container config to `CreateContainer()` CRI call:

```yaml
  devices:
  - container_path: /dev/testpvc
    host_path: /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~local-volume/local-block-pv
    permissions: mrw
```

Virtlet can use `host_path` to attach the device to the VM using a
`DomainDisk`, and `container_path` to mount it inside the VM using
cloud-init. The handling of local and non-local PVs doesn't differ
on the CRI level.

Supporting non-local PVs will automatically give Virtlet support for
all the Kubernetes volume types that support the block mode, which
include Ceph, FibreChannel, and the persistent disks on AWS, GCP and
Azure, with the list probably growing larger in the future. It will
also give automatic support for CSI plugins that support the block
mode.  The caveat is that the block mode is Alpha as of Kubernetes
1.10 and it wasn't checked for earlier Kubernetes versions.

The use of block PVs will eliminate the need for custom flexvolumes at
some point (after block volumes become GA and we stop supporting
earlier Kubernetes versions). There's one caveat, with block PVs the
Ceph RBDs will be mapped on the nodes by `kubelet`, instead of being
consumed by qemu by the means of `librbd`. It's not clear though if
this will be good or bad from the performance standpoint. If we'll
still need custom volume types, flexvolumes may be replaced with
[CSI](https://kubernetes.io/blog/2018/04/10/container-storage-interface-beta/).

More advantages of using block PVs instead of custom flexvolumes
include having VM pods differ even less from "plain" pods, and a
possibility to make use automatic PV provisioning in future.

There's also a possibility of using the block PVs (local or non-local)
for the persistent rootfs. It's possible to copy the image onto PV
upon the first use, and then have another pod reuse the PV after the
original one is destroyed. For local PVs, the scheduler will always
place the pod on the node where the local PV resides (this constitutes
so called "gravity"). There's a problem with this approach, namely,
there's no reliable way for a CRI implementation to find a PV that
corresponds to a block device, so Virtlet will have to examine the
contents of the PV to see if it's used for the first time. This also
means that Virtlet will have hard time establishing the correspondence
between PVs and the images that are applied to them (e.g. imagine a PV
being used by a pod with different image later). It's possible to
overcome these problems by either storing the metadata on the block
device itself somehow, or using CRDs and PV metadata to keep track of
"pet" VMs and their root filesystems. The use of local PVs will take
much of the burden from the corresponding controller, though.

## Experimenting with the Local Persistent Volumes

First, we need to define a storage class that specifies
`volumeBindingMode: WaitForFirstConsumer` that's
[needed](https://kubernetes.io/blog/2018/04/13/local-persistent-volumes-beta/#enabling-smarter-scheduling-and-volume-binding)
for propoper pod scheduling:
```yaml
---
kind: StorageClass
apiVersion: storage.k8s.io/v1
metadata:
  name: local-storage
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
```

Below is a definition of a Local Persistent Volume:
```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: local-block-pv
spec:
  capacity:
    storage: 100Mi
  accessModes:
  - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  storageClassName: local-storage
  volumeMode: Block
  local:
    path: /dev/loop3
  claimRef:
    name: local-block-pvc
    namespace: default
  nodeAffinity:
    required:
      nodeSelectorTerms:
      - matchExpressions:
        - key: kubernetes.io/hostname
          operator: In
          values:
          - kube-node-1
```

The important parts here are the following: `volumeMode: Block`
setting the block volume mode, local volume source specification
that makes the PV use `/dev/loop3`
```yaml
  local:
    path: /dev/loop3
```
and a `nodeAffinity` spec that pins the local PV to `kube-node-1`:
```
  nodeAffinity:
    required:
      nodeSelectorTerms:
      - matchExpressions:
        - key: kubernetes.io/hostname
          operator: In
          values:
          - kube-node-1
```

The following PVC makes use of that PV (it's referenced explicitly via
`claimRef` above but we could allow Kubernetes to associate the PV
with PVC instead), also including `volumeMode: Block` in it:
```yaml
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: local-block-pvc
spec:
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
  storageClassName: local-storage
  resources:
    requests:
      storage: 100Mi
```

And, finally, a pod that makes use of the PVC:
```
---
kind: Pod
apiVersion: v1
metadata:
  name: test-block-pod
spec:
  containers:
  - name: ubuntu
    image: ubuntu:16.04
    command:
    - /bin/sh
    - -c
    - sleep 30000
    volumeDevices:
    - devicePath: /dev/testpvc
      name: testpvc
  volumes:
  - name: testpvc
    persistentVolumeClaim:
      claimName: local-block-pvc
```

In the pod definition, we're using `volumeDevices` with `devicePath`
instead of `volumeMounts` with `mountPath`. This will make the node's
`/dev/loop3` appear as `/dev/testpvc` inside the pod's container:

```
$ kubectl exec test-block-pod -- ls -l /dev/testpvc
brw-rw---- 1 root disk 7, 3 Jun 12 20:44 /dev/testpvc
$ kubectl exec test-block-pod -- mkfs.ext4 /dev/testpvc
Discarding device blocks: done
Creating filesystem with 102400 1k blocks and 25688 inodes
Filesystem UUID: a02f7560-23a6-45c1-b10a-6e0a1b1eee72
Superblock backups stored on blocks:
        8193, 24577, 40961, 57345, 73729

Allocating group tables: done
Writing inode tables: done
Creating journal (4096 blocks): mke2fs 1.42.13 (17-May-2015)
done
Writing superblocks and filesystem accounting information: done
```

The important part is that the pod gets automatically scheduled on the
node where the local PV used by the PVC resides:
```
$ kubectl get pods test-block-pod -o wide
NAME             READY     STATUS    RESTARTS   AGE       IP           NODE
test-block-pod   1/1       Running   0          21m       10.244.2.9   kube-node-1
```

From CRI point of view, the following container config is passed to
the `CreateContainer()` call, as seen in CRI Proxy logs (pod sandbox
config omitted for brevity as it doesn't contain the mount or device
related information):
```yaml
I0612 20:44:29.869566    1038 proxy.go:126] ENTER: /runtime.v1alpha2.RuntimeService/CreateContainer():
config:
  annotations:
    io.kubernetes.container.hash: ff82c6d3
    io.kubernetes.container.restartCount: "0"
    io.kubernetes.container.terminationMessagePath: /dev/termination-log
    io.kubernetes.container.terminationMessagePolicy: File
    io.kubernetes.pod.terminationGracePeriod: "30"
  command:
  - /bin/sh
  - -c
  - sleep 30000
  devices:
  - container_path: /dev/testpvc
    host_path: /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~local-volume/local-block-pv
    permissions: mrw
  envs:
  - key: KUBERNETES_SERVICE_PORT_HTTPS
    value: "443"
  - key: KUBERNETES_PORT
    value: tcp://10.96.0.1:443
  - key: KUBERNETES_PORT_443_TCP
    value: tcp://10.96.0.1:443
  - key: KUBERNETES_PORT_443_TCP_PROTO
    value: tcp
  - key: KUBERNETES_PORT_443_TCP_PORT
    value: "443"
  - key: KUBERNETES_PORT_443_TCP_ADDR
    value: 10.96.0.1
  - key: KUBERNETES_SERVICE_HOST
    value: 10.96.0.1
  - key: KUBERNETES_SERVICE_PORT
    value: "443"
  image:
    image: sha256:5e8b97a2a0820b10338bd91674249a94679e4568fd1183ea46acff63b9883e9c
  labels:
    io.kubernetes.container.name: ubuntu
    io.kubernetes.pod.name: test-block-pod
    io.kubernetes.pod.namespace: default
    io.kubernetes.pod.uid: 65b0c985-6e81-11e8-be27-769e6e14e66a
  linux:
    resources:
      cpu_shares: 2
      oom_score_adj: 1000
    security_context:
      namespace_options:
        pid: 1
      run_as_user: {}
  log_path: ubuntu/0.log
  metadata:
    name: ubuntu
  mounts:
  - container_path: /var/run/secrets/kubernetes.io/serviceaccount
    host_path: /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/volumes/kubernetes.io~secret/default-token-7zwlh
    readonly: true
  - container_path: /etc/hosts
    host_path: /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/etc-hosts
  - container_path: /dev/termination-log
    host_path: /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/containers/ubuntu/2be42601
```

The important part is this:
```yaml
  devices:
  - container_path: /dev/testpvc
    host_path: /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~local-volume/local-block-pv
    permissions: mrw
```

If we look at the node, we'll see that `host_path` points to a symlink to `/dev/loop3` which
is specified in the local block PV:
```
root@kube-node-1:/# ls -l /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~local-volume/local-block-pv
lrwxrwxrwx 1 root root 10 Jun 13 08:31 /var/lib/kubelet/pods/65b0c985-6e81-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~local-volume/local-block-pv -> /dev/loop3
```

`container_path` denotes the path to the device inside the container.

The `permissions` is described in CRI spec as follows:
```
    // Cgroups permissions of the device, candidates are one or more of
    // * r - allows container to read from the specified device.
    // * w - allows container to write to the specified device.
    // * m - allows container to create device files that do not yet exist.
```

Also note that the device is not listed in `mounts`.

There's a
[tool](https://github.com/kubernetes-incubator/external-storage/tree/master/local-volume)
for automatic provisioning of Local Persistent Volumes that's part of
[external-storage](https://github.com/kubernetes-incubator/external-storage)
project. Right now it may not be very useful for Virtlet, but it may
gain some important features later, like support for automatic
partitioning and fs formatting.

## Experimenting with non-local ("plain") Persistent Volumes

Let's check "plain" PVs now. We'll be using Ceph block volumes.

Below are some tricks that make kubeadm-dind-cluster compatible with
Ceph. Some of them may be useful for running
[Rook](https://github.com/rook/rook) on k-d-c, too.

For Ceph RBDs to work with Kubernetes Ceph PVs (not just Virtlet's
flexvolume-based ones), I had to make `rbd` work on the DIND nodes, so
the following change had to be made to the kubeadm-dind-cluster's main
script (observed in [Rook's](https://github.com/rook/rook) DIND
setup):
```
diff --git a/dind-cluster.sh b/dind-cluster.sh
index e9118e2..24a0a78 100755
--- a/dind-cluster.sh
+++ b/dind-cluster.sh
@@ -645,6 +645,9 @@ function dind::run {
          --hostname "${container_name}" \
          -l mirantis.kubeadm_dind_cluster \
          -v ${volume_name}:/dind \
+         -v /dev:/dev \
+         -v /sys/bus:/sys/bus \
+         -v /var/run/docker.sock:/opt/outer-docker.sock \
          ${opts[@]+"${opts[@]}"} \
          "${DIND_IMAGE}" \
          ${args[@]+"${args[@]}"}
```

The following file had to be added as a fake `rbd` command to each DIND node
(borrowed from [Rook scripts](https://github.com/rook/rook/blob/cd2b69915958e7453b3fc5031f59179058163dcd/tests/scripts/dind-cluster-rbd)):
```
#!/bin/bash
DOCKER_HOST=unix:///opt/outer-docker.sock /usr/bin/docker run --rm -v /sys:/sys --net=host --privileged=true ceph/base rbd "$@"
```
It basically executes rbd command using `ceph/base` images using the
host docker in the host network namespace.

So let's bring up the cluster:
```bash
./dind-cluster.sh up
```

Disable rate limiting so journald doesn't choke on CRI proxy logs on the node 1:
```bash
docker exec kube-node-1 /bin/bash -c 'echo "RateLimitInterval=0" >>/etc/systemd/journald.conf && systemctl restart systemd-journald'
```

Enable `BlockVolume` mode for kubelet on the node 1
(`MountPropagation` is enabled by default in 1.10, so let's just
replace it):
```bash
docker exec kube-node-1 /bin/bash -c 'sed -i "s/MountPropagation/BlockVolume/" /lib/systemd/system/kubelet.service && systemctl daemon-reload && systemctl restart kubelet'
```

Install CRI Proxy so we can grab the logs:
```bash
CRIPROXY_DEB_URL="${CRIPROXY_DEB_URL:-https://github.com/Mirantis/criproxy/releases/download/v0.11.0/criproxy-nodeps_0.11.0_amd64.deb}"
docker exec kube-node-1 /bin/bash -c "curl -sSL '${CRIPROXY_DEB_URL}' >/criproxy.deb && dpkg -i /criproxy.deb && rm /criproxy.deb"
```

Taint node 2 so we get everything scheduled on node 1:
```bash
kubectl taint nodes kube-node-2 dedicated=foobar:NoSchedule
```

Now we need to add `rbd` command to the 'hypokube' image that's used
by the control plane (we need it for `kube-controller-manager`). The
proper way would be using the node's `rbd` command with mounting host
docker socket into the container, but as the controller manager
doesn't need `rbd map` command which needs host access, we can just
install `rbd` package here, just make sure it's new enough to support
commands like `rbd status` that are invoked by the controller manager:

```bash
docker exec kube-master /bin/bash -c 'docker rm -f tmp; docker run --name tmp mirantis/hypokube:final /bin/bash -c "echo deb http://ftp.debian.org/debian jessie-backports main >>/etc/apt/sources.list && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y ceph-common=10.2.5-6~bpo8+1 libradosstriper1 ntp librados2=10.2.5-6~bpo8+1 librbd1=10.2.5-6~bpo8+1 python-cephfs=10.2.5-6~bpo8+1 libcephfs1=10.2.5-6~bpo8+1" && docker commit tmp mirantis/hypokube:final && docker rm -f tmp'
```

At this point, we must edit the following files on `kube-master` node, adding
`--feature-gates=BlockVolume=true` to the end of `command:` in each pod's only container:

* `/etc/kubernetes/manifests/kube-apiserver.yaml`
* `/etc/kubernetes/manifests/kube-scheduler.yaml`
* `/etc/kubernetes/manifests/kube-controller-manager.yaml`

Likely, updating just the controller manager may suffice, but I didn't
check.  This will cause the pods to restart and use the updated
`mirantis/hypokube:final` image.

Now let's start the Ceph demo container:
```bash
MON_IP=$(docker exec kube-master route | grep default | awk '{print $2}')
CEPH_PUBLIC_NETWORK=${MON_IP}/16
docker run -d --net=host -e MON_IP=${MON_IP} \
       -e CEPH_PUBLIC_NETWORK=${CEPH_PUBLIC_NETWORK} \
       --name ceph_cluster docker.io/ceph/demo
```

Create a pool there:
```bash
docker exec ceph_cluster ceph osd pool create kube 8 8
```

Create an image for testing (it's important to use `rbd create` with
`layering` feature here so as not to get a feature mismatch error
later when creating a pod):
```bash
docker exec ceph_cluster rbd create tstimg \
       --size 11M --pool kube --image-feature layering
```

Set up a Kubernetes secret for use with Ceph:
```bash
admin_secret="$(docker exec ceph_cluster ceph auth get-key client.admin)"
kubectl create secret generic ceph-admin \
        --type="kubernetes.io/rbd" \
        --from-literal=key="${admin_secret}" \
        --namespace=kube-system
```

Copy the `rbd` replacement script presented earlier above to each node:
```bash
for n in kube-{master,node-{1,2}}; do
  docker cp dind-cluster-rbd ${n}:/usr/bin/rbd
done
```

Now we can create a test PV, PVC and a pod.

Let's define a storage class:
```yaml
kind: StorageClass
apiVersion: storage.k8s.io/v1
metadata:
  name: ceph-testnew
provisioner: kubernetes.io/rbd
parameters:
  monitors: 10.192.0.1:6789
  adminId: admin
  adminSecretName: ceph-admin
  adminSecretNamespace: kube-system
  pool: kube
  userId: admin
  userSecretName: ceph-admin
  userSecretNamespace: kube-system
  fsType: ext4
  imageFormat: "1"
  # the following was disabled while testing non-block PVs
  imageFeatures: "layering"
```
Actually, the automatic provisioning didn't work for me because it
was setting `volumeMode: Filesystem` in the PVs, but this was probably
due to my mistake, or otherwise from looking at Kubernetes source
it should be fixable.

Let's define a block PV:
```yaml
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: test-block-pv
spec:
  accessModes:
  - ReadWriteOnce
  capacity:
    storage: 10Mi
  claimRef:
    name: ceph-block-pvc
    namespace: default
  persistentVolumeReclaimPolicy: Delete
  rbd:
    image: tstimg
    keyring: /etc/ceph/keyring
    monitors:
    - 10.192.0.1:6789
    pool: kube
    secretRef:
      name: ceph-admin
      namespace: kube-system
    user: admin
  storageClassName: ceph-testnew
  volumeMode: Block
```

The difference from the "usual" RBD PV is `volumeMode: Block` here,
and the same goes for the PVC:
```yaml
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: ceph-block-pvc
spec:
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
  storageClassName: ceph-testnew
  resources:
    requests:
      storage: 10Mi
```

Now, the pod itself, with `volumeDevices` instead of `volumeMounts`:
```yaml
kind: Pod
apiVersion: v1
metadata:
  name: ceph-block-pod
spec:
  containers:
  - name: ubuntu
    image: ubuntu:16.04
    command:
    - /bin/sh
    - -c
    - sleep 30000
    volumeDevices:
    - name: data
      devicePath: /dev/cephdev
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: ceph-block-pvc
```

Let's do `kubectl apply -f ceph-test.yaml` (`ceph-test.yaml`
containing all of the yaml documents above), and try it out:

```
$ kubectl exec ceph-block-pod -- ls -l /dev/cephdev
brw-rw---- 1 root disk 252, 0 Jun 12 20:19 /dev/cephdev
$ kubectl exec ceph-block-pod -- mkfs.ext4 /dev/cephdev
mke2fs 1.42.13 (17-May-2015)
Discarding device blocks: done
Creating filesystem with 11264 1k blocks and 2816 inodes
Filesystem UUID: 81ce32e8-bf37-4bc8-88bf-674bf6f79d14
Superblock backups stored on blocks:
        8193

Allocating group tables: done
Writing inode tables: done
Creating journal (1024 blocks): done
Writing superblocks and filesystem accounting information: done
```

Let's capture CRI Proxy logs:
```
docker exec kube-node-1 journalctl -xe -n 20000 -u criproxy|egrep --line-buffered -v '/run/virtlet.sock|\]: \{\}|/var/run/dockershim.sock|ImageFsInfo' >/tmp/log.txt
```

The following is the important part of the log which is slightly
cleaned up:
```
I0612 20:19:38.681852    1038 proxy.go:126] ENTER: /runtime.v1alpha2.RuntimeService/CreateContainer():
config:
  annotations:
    io.kubernetes.container.hash: d0c4a380
    io.kubernetes.container.restartCount: "0"
    io.kubernetes.container.terminationMessagePath: /dev/termination-log
    io.kubernetes.container.terminationMessagePolicy: File
    io.kubernetes.pod.terminationGracePeriod: "30"
  command:
  - /bin/sh
  - -c
  - sleep 30000
  devices:
  - container_path: /dev/cephdev
    host_path: /var/lib/kubelet/pods/ebb11dcb-6e7d-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~rbd/test-block-pv
    permissions: mrw
  envs:
  - key: KUBERNETES_PORT
    value: tcp://10.96.0.1:443
  - key: KUBERNETES_PORT_443_TCP
    value: tcp://10.96.0.1:443
  - key: KUBERNETES_PORT_443_TCP_PROTO
    value: tcp
  - key: KUBERNETES_PORT_443_TCP_PORT
    value: "443"
  - key: KUBERNETES_PORT_443_TCP_ADDR
    value: 10.96.0.1
  - key: KUBERNETES_SERVICE_HOST
    value: 10.96.0.1
  - key: KUBERNETES_SERVICE_PORT
    value: "443"
  - key: KUBERNETES_SERVICE_PORT_HTTPS
    value: "443"
  image:
    image: sha256:5e8b97a2a0820b10338bd91674249a94679e4568fd1183ea46acff63b9883e9c
  labels:
    io.kubernetes.container.name: ubuntu
    io.kubernetes.pod.name: ceph-block-pod
    io.kubernetes.pod.namespace: default
    io.kubernetes.pod.uid: ebb11dcb-6e7d-11e8-be27-769e6e14e66a
  linux:
    resources:
      cpu_shares: 2
      oom_score_adj: 1000
    security_context:
      namespace_options:
        pid: 1
      run_as_user: {}
  log_path: ubuntu/0.log
  metadata:
    name: ubuntu
  mounts:
  - container_path: /var/run/secrets/kubernetes.io/serviceaccount
    host_path: /var/lib/kubelet/pods/ebb11dcb-6e7d-11e8-be27-769e6e14e66a/volumes/kubernetes.io~secret/default-token-7zwlh
    readonly: true
  - container_path: /etc/hosts
    host_path: /var/lib/kubelet/pods/ebb11dcb-6e7d-11e8-be27-769e6e14e66a/etc-hosts
  - container_path: /dev/termination-log
    host_path: /var/lib/kubelet/pods/ebb11dcb-6e7d-11e8-be27-769e6e14e66a/containers/ubuntu/577593a5
```

Again, we have this here:
```yaml
  devices:
  - container_path: /dev/cephdev
    host_path: /var/lib/kubelet/pods/ebb11dcb-6e7d-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~rbd/test-block-pv
    permissions: mrw
```

The `host_path` points to a mapped RBD:
```yaml
root@kube-node-1:/# ls -l /var/lib/kubelet/pods/ebb11dcb-6e7d-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~rbd/test-block-pv
lrwxrwxrwx 1 root root 9 Jun 12 20:19 /var/lib/kubelet/pods/ebb11dcb-6e7d-11e8-be27-769e6e14e66a/volumeDevices/kubernetes.io~rbd/test-block-pv -> /dev/rbd0
```

An unpleasant part about RBDs+DIND is that the machine may hang on
some commands / refuse to reboot if RBDs aren't unmapped. If kdc
cluster is already teared down (but `ceph_cluster` container is still
alive), the following commands can be used to list and unmap RBDs on
the Linux host:

```
# rbd showmapped
id pool image                                 snap device
0  kube tstimg                                -    /dev/rbd0
# rbd unmap -o force kube/tstimg
```
