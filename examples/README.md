# Virtlet pod example

In order to try out the example, do the following on a cluster that
has nodes with Virtlet on it (see [the instructions](../deploy/README.md) in
`deploy/` directory):

1. Create a sample VM:
```bash
kubectl create -f cirros-vm.yaml
```
2. Wait for `cirros-vm` pod to become `Running`:
```bash
kubectl get pods -w
```
3. Connect to the VM console:
```bash
kubectl attach -it cirros-vm
```
4. As soon as the VM has booted, you can use
[virtletctl tool](../docs/virtletctl.md) (available as part
of each Virtlet release on GitHub starting from Virtlet 1.0):

```bash
virtletctl ssh cirros@cirros-vm -- -i examples/vmkey [command...]
```

Besides [cirros-vm.yaml](cirros-vm.yaml), there's also [ubuntu-vm.yaml](ubuntu-vm.yaml) that can be used to start an Ubuntu Xenial VM and [fedora-vm.yaml](fedora-vm.yaml) that starts a Fedora VM. These VMs can also be accessed using `virtletctl ssh` after it boots:
```bash
virtletctl ssh ubuntu@ubuntu-vm -- -i examples/vmkey [command...]
virtletctl ssh fedora@fedora-vm -- -i examples/vmkey [command...]
```

# Kubernetes on VM-based StatefulSet

[Another example](k8s.yaml) involves starting several VMs using `StatefulSet` and deploying
Kubernetes using `kubeadm` on it.

You can create the cluster like this:
```bash
kubectl create -f k8s.yaml
```

Watch progress of the cluster setup via the VM console:
```bash
kubectl attach -it k8s-0
```

After it's complete you can log into the master node:

```bash
virtletctl ssh root@k8s-0 -- -i examples/vmkey
```

There you can wait a bit for k8s nodes and pods to become ready.
You can list them using the following commands inside the VM:

```bash
kubectl get nodes -w
# Press Ctrl-C when all 3 nodes are present and Ready
kubectl get pods --all-namespaces -o wide -w
# Press Ctrl-C when all the pods are ready
```

You can then deploy and test nginx on the inner cluster:

```bash
kubectl run nginx --image=nginx --expose --port 80
kubectl get pods -w
# Press Ctrl-C when the pod is ready
kubectl run bbtest --rm --attach --image=docker.io/busybox --restart=Never -- wget -O - http://nginx
```

After that you can follow
[the instructions](../deploy/real-cluster.md) to install Virtlet on
the cluster if you want, but note that you'll have to disable KVM
because nested virtualization is not yet supported by Virtlet.

# Using local block PVs

To use the block PV examples, you need to enable `BlockVolume`
[feature gate](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/)
for your Kubernetes cluster components. When using
[kubeadm-dind-cluster](https://github.com/Mirantis/kubeadm-dind-cluster)
for testing, you can use this command to start the cluster with
`BlockVolume` and Ceph support:
```bash
FEATURE_GATES="BlockVolume=true" \
    KUBELET_FEATURE_GATES="BlockVolume=true" \
    ENABLE_CEPH=1 \
    ./dind-cluster-v1.14.sh up
```

[ubuntu-vm-local-block-pv.yaml](ubuntu-vm-local-block-pv.yaml)
demonstrates the use of local block volumes. For the sake of
simplicity, it uses a file named `/var/lib/virtlet/looptest` instead
of a real block device, but from the user perspective the usage is the
same except that `/dev/...` path must be specified instead of
`/var/lib/virtlet/looptest` in the most real-world use cases.  The
path is chosen to be under `/var/lib/virtlet` because this directory
is mounted into the Virtlet pod by default and Virtlet must have
access to the file or block device specified for the block PV.
First, you need to create the file to be used for the contents
of the local block PV:
```bash
docker exec kube-node-1 dd if=/dev/zero of=/var/lib/virtlet/looptest bs=1M count=1000
docker exec kube-node-1 mkfs.ext4 /var/lib/virtlet/looptest
```

Let's create the PV, PVC and the pod that uses them:
```bash
kubectl apply -f examples/ubuntu-vm-local-block-pv.yaml
```

After the VM boots, we can log into it and verify that the PV is
indeed mounted:

```console
$ virtletctl ssh ubuntu@ubuntu-vm -- -i examples/vmkey
...
ubuntu@ubuntu-vm:~$ sudo touch /mnt/foo
ubuntu@ubuntu-vm:~$ ls -l /mnt
total 16
-rw-r--r-- 1 root root     0 Oct  1 17:27 foo
drwx------ 2 root root 16384 Oct  1 14:41 lost+found
$ exit
```

Then we can delete and re-create the pod
```bash
kubectl delete pod ubuntu-vm
# wait till the pod disappears
kubectl get pod -w
kubectl apply -f examples/ubuntu-vm-local-block-pv.yaml
```

And, after the VM boots, log in again to verify that the file `foo` is
still there:
```console
$ virtletctl ssh ubuntu@ubuntu-vm -- -i examples/vmkey
...
ubuntu@ubuntu-vm:~$ ls -l /mnt
total 16
-rw-r--r-- 1 root root     0 Oct  1 17:27 foo
drwx------ 2 root root 16384 Oct  1 14:41 lost+found
$ exit
```

# Using Ceph block PVs

For Ceph examples you'll also need to start a Ceph test container
(`--privileged` flag and `-v` mounts of `/sys/bus` and `/dev` are
needed for `rbd map` to work from within the `ceph_cluster` container;
they're not needed for persistent root filesystem example in the next
section):
```bash
MON_IP=$(docker exec kube-master route | grep default | awk '{print $2}')
CEPH_PUBLIC_NETWORK=${MON_IP}/16
docker run -d --net=host -e MON_IP=${MON_IP} \
       --privileged \
       -v /dev:/dev \
       -v /sys/bus:/sys/bus \
       -e CEPH_PUBLIC_NETWORK=${CEPH_PUBLIC_NETWORK} \
       -e CEPH_DEMO_UID=foo \
       -e CEPH_DEMO_ACCESS_KEY=foo \
       -e CEPH_DEMO_SECRET_KEY=foo \
       -e CEPH_DEMO_BUCKET=foo \
       -e DEMO_DAEMONS="osd mds" \
       --name ceph_cluster docker.io/ceph/daemon demo
# wait for the cluster to start
docker logs -f ceph_cluster
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
       --size 1G --pool kube --image-feature layering
```

Set up a Kubernetes secret for use with Ceph:
```bash
admin_secret="$(docker exec ceph_cluster ceph auth get-key client.admin)"
kubectl create secret generic ceph-admin \
        --type="kubernetes.io/rbd" \
        --from-literal=key="${admin_secret}"
```

To test the block PV, we also need to create a filesystem on the node
(this is not needed for testing the persistent rootfs below).
Yo may need to load RBD module on the docker host to be able to do this:
```bash
modprobe rbd
```

Then we can map the RBD, create a filesystem on it and unmap it again:
```bash
rbd=$(docker exec ceph_cluster rbd map tstimg --pool=kube)
docker exec kube-node-1 mkfs.ext4 "${rbd}"
docker exec ceph_cluster rbd unmap tstimg --pool=kube
```

After that, you can create the block PV, PVC and the pod that uses
them and verify the PV being mounted into `ubuntu-vm` the same way as
it was done in the previous section:

```bash
kubectl apply -f examples/ubuntu-vm-rbd-block-pv.yaml
```

# Using the persistent root filesystem

[cirros-vm-persistent-rootfs-local.yaml](cirros-vm-persistent-rootfs-local.yaml)
demonstrates the use of persistent root filesystem. The most important part
is the `volumeDevices` section in the pod's container definition:
```yaml
    volumeDevices:
    - devicePath: /
      name: testpvc
```

Unlike the local PV example above, we can't use a file instead of a
real block device, as Virtlet uses the device mapper internally which
can't work with plain files. We don't need to run `mkfs.ext4` this
time though as Virtlet will copy the VM image over the contents of the
device. Let's create a loop device to be used for the PV:

```bash
docker exec kube-node-1 dd if=/dev/zero of=/rawtest bs=1M count=1000
docker exec kube-node-1 /bin/bash -c 'ln -s $(losetup -f /rawtest --show) /dev/rootdev'
```
We use a symbolic link to the actual block device here so we don't
need to edit the example yaml.


After that, we create the PV, PVC and the pod:
```bash
kubectl apply -f examples/cirros-vm-persistent-rootfs-local.yaml
```

After the VM boots, we can log into it and create a file on the root
filesystem:

```console
$ virtletctl ssh cirros@cirros-vm-pl -- -i examples/vmkey
...
$ echo foo >bar.txt
```

Then we delete the pod, wait for it to disappear, and then re-create it:
```bash
kubectl delete pod cirros-vm-p
kubectl apply -f examples/cirros-vm-persistent-rootfs-local.yaml
```

After logging into the new VM pod, we see that the file is still
there:
```console
$ virtletctl ssh cirros@cirros-vm-pl -- -i examples/vmkey
...
$ cat bar.txt
foo
```

[cirros-vm-persistent-rootfs-rbd.yaml](cirros-vm-persistent-rootfs-rbd.yaml)
demonstrates the use of persistent root filesystem on a Ceph RBD.  To
use it, you need to set up a test Ceph cluster and create a test image
as described in the [previous section](#using-ceph-block-pvs), except
that you don't have to run the Ceph test container as `--privileged`,
don't have to mount `/dev` and `/sys/bus` into the Ceph test container
and don't have to map the RBD and run `mkfs.ext4` on it. You can
create the PV, PVC and the pod for the example using this command:
```bash
kubectl apply -f examples/cirros-vm-persistent-rootfs-rbd.yaml
```

After that, you can verify that the persistent rootfs indeed works
using the same approach as with local PVs, but using name `cirros-vm-pr`
in place of `cirros-vm-pl`.
