# The problem of fast persistent rootfs setup

Right now, the persistent rootfs for VMs is initialized by the means
of `qemu-img convert` which converts a QCOW2 image to raw one and
writes the result over a block device mapped on the host. It's
possible to overcome the problem by utilizing the new persistent
volume snapshotting feature in Kubernetes 1.12. It's also possible to
implement a solution for VMs which use local libvirt volume as their
root filesystem. In both cases, we'll need to add a CRD and a
controller for managing persistent root filesystems.

## Defining VM identities

To support faster rootfs initialization, we need to introduce the
concept of VM Identity. VM Identities are defined like this:

```yaml
---
apiVersion: "virtlet.k8s/v1"
kind: VirtletVMIdentitySet
metadata:
  name: test-identity-set
spec:
  # specify the image to use. sha256 digest is required here
  # to avoid possible inconsistencies.
  image: virtlet.cloud/foobar-image@sha256:a8dd75ecffd4cdd96072d60c2237b448e0c8b2bc94d57f10fdbc8c481d9005b8
  # specify SMBIOS UUID (optional). If the UUID is specified,
  # only single VM at a time can utilize this IdentitySet
  firmwareUUID: 1b4d298f-6151-40b0-a1d4-269fc41d48f0
  # specify the type of VM identity:
  #   'Local' for libvirt-backed identities
  #   'PersistentVolume' fo PV-backed identities
  type: Local
  # specify the size to use, defaults to the virtual size
  # of the image
  size: 1Gi
  # for non-local PVs, storageClassName specifies the storage
  # class to use
  # storageClassName: csi-rbd
```

and they can be associated with VM pods like this:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: persistent-vm
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
    VirtletVMIdentitySet: test-identity-set
    # uncomment to use instead of the pod name
    # VirtletVMIdentityName: override-identity-name
spec:
  ...
  containers:
  - name: vm
    # must match the image specified in test-identity
    image: virtlet.cloud/foobar-image@sha256:a8dd75ecffd4cdd96072d60c2237b448e0c8b2bc94d57f10fdbc8c481d9005b8
...
```

The VM Identity Sets and VM Identity objects which are generated from
them are managed by the VM Identity Controller which is created as
a Deployment from `virtletctl`-generated yaml.

The VM identity object looks like this:
```yaml
apiVersion: "virtlet.k8s/v1"
kind: VirtletVMIdentity
metadata:
  name: persistent-vm
spec:
  # spec fields are copied from VMIdentitySet
  image: virtlet.cloud/foobar-image@sha256:a8dd75ecffd4cdd96072d60c2237b448e0c8b2bc94d57f10fdbc8c481d9005b8
  firmwareUUID: 1b4d298f-6151-40b0-a1d4-269fc41d48f0
  type: Local
  size: 1Gi
status:
  creationTime: 2018-10-19T06:07:40Z
  # 'ready' field is true when this identity is
  # ready for using in a VM. Virtlet will delay
  # the actual startup of the pod till the identity
  # is ready.
  ready: true
```

When a pod is created, an VM Identity object is instantiated if it
doesn't exist. The VM Identity objects are CRDs that have their own
name (as all k8s objects do) and a pointer to `VMIdentitySet`. The
name of the VM Identity object defaults to that of the pod unless
`VirtletVMIdentityName` annotation is specified. This extra level
of indirection is needed so as to enable VM identities for
`StatefulSets`

VM Identity controller takes care of managing PVCs corresponding to
the identities as well as asking local Virtlet processes to make
libvirt volumes.

## Using libvirt-backed identities

The libvirt-backed VM identity objects correspond to libvirt volumes
that have QCOW2 images as backing files. When such identity object is
deleted, the volume is deleted, too. When a VM pod is created on a
host other than one currently hosting the corresponding libvirt
volume, the volume is moved to the target host. This enables offline
migrations of the VMs with libvirt-based persistent root filesystem.

The basic idea of orchestrating the lifecycle of libvirt volumes
corresponding to VM identities is having VM Identity controller
contact virtlet process on the node in order to make it perform the
actions on libvirt volume objects.

## Using persistent volume snapshots

Kubernetes v1.12 adds support for
[persistent volume snapshots](https://kubernetes.io/blog/2018/10/09/introducing-volume-snapshot-alpha-for-kubernetes/)
for CSI, which is supported by Ceph CSI driver among others. We can
keep a pool of snapshots that correspond to different images. After
that, we can create new PVs from these snapshots, resizing them if
necessary (we should recheck that such resizes are possible after
snapshotting stabilizes for Ceph CSI).

There are some issues with current Ceph CSI and the snapshotter, see
[Appendix A](#appendix-a-experimenting-with-ceph-csi-and-external-snapshotter)
for details.

We'll be dealing with Ceph CSI in the description below.

For each image with a hash mentioned in a VM identity, Virtlet will
create a PVC, wait for it to be provisioned, write the raw image
contents to it and then make a snapshot. Once an image stops being
used, the corresponding PVC, PV and the snapshots are automatically
removed (implementation note: this can probably be achieved by the
means of an extra CRD for the image plus Kubernetes ownership
mechanisms). Then, for each identity, a new final PV is made based on
the snapshot. The final PVs are deleted when the identity disappears.

## More possibilities

We can consider extending
[LVM2 CSI plusin](https://github.com/mesosphere/csilvm) to support
snapshots so it can be used for local PV snapshots.

## Further ideas

We can auto-create a 1-replica StatefulSet for identities if the user
wants it. This will make VM pods "hidden" and we'll have a CRD for
"pet" VMs.

We can also generate SMBIOS UUIDs for the pods of multi-replica
StatefulSets using UUIDv5 (a hash based on an UUID from
VirtletVMIdentitySet yaml and the name of the pod).

## Appendix A. Experimenting with Ceph CSI and external-snapshotter

For this experiment, kubeadm-dind-cluster with k8s 1.12 was used.
The following settings were applied:
```console
$ export FEATURE_GATES="BlockVolume=true,CSIBlockVolume=true,VolumeSnapshotDataSource=true"
$ export KUBELET_FEATURE_GATES="BlockVolume=true,CSIBlockVolume=true,VolumeSnapshotDataSource=true"
$ export ENABLE_CEPH=1
```

We'll need ceph-csi:
```console
$ git clone https://github.com/ceph/ceph-csi.git
```

We'll be using `external-snapshotter` for snapshots. For me, it was
having some gRPC errors probably due to the race with csi socket
initialization. I had to add a container with `sleep` command
to CSI RBD plugin yaml:

```
diff --git a/deploy/rbd/kubernetes/csi-rbdplugin.yaml b/deploy/rbd/kubernetes/csi-rbdplugin.yaml
index d641a78..c8da074 100644
--- a/deploy/rbd/kubernetes/csi-rbdplugin.yaml
+++ b/deploy/rbd/kubernetes/csi-rbdplugin.yaml
@@ -38,6 +38,22 @@ spec:
               mountPath: /var/lib/kubelet/plugins/csi-rbdplugin
             - name: registration-dir
               mountPath: /registration
+        - name: csi-snapshotter
+          image: quay.io/k8scsi/csi-snapshotter:v0.4.0
+          command:
+          - /bin/sh
+          - -c
+          - "sleep 60000"
+          # args:
+          #   - "--csi-address=$(CSI_ENDPOINT)"
+          #   - "--connection-timeout=15s"
+          env:
+            - name: CSI_ENDPOINT
+              value: unix://var/lib/kubelet/plugins/csi-rbdplugin/csi.sock
+          imagePullPolicy: Always
+          volumeMounts:
+            - name: plugin-dir
+              mountPath: /var/lib/kubelet/plugins/csi-rbdplugin
         - name: csi-rbdplugin
           securityContext:
             privileged: true
```

Deploy Ceph CSI plugin:
```console
$ ./plugin-deploy.sh
```

And after deploying Ceph CSI plugin I had to start the snapshotter
manually from within `csi-snapshotter` container of the csi-rbdplugin
pod

```console
$ kubectl exec -it -c csi-snapshotter csi-rbdplugin-zwjl2 /bin/sh
$ /csi-snapshotter --logtostderr --csi-address /var/lib/kubelet/plugins/csi-
rbdplugin/csi.sock --connection-timeout=15s --v=10
```

As a quick hack (for testing only!), you can dumb down RBAC to make it
work:
```console
kubectl create clusterrolebinding permissive-binding   \
        --clusterrole=cluster-admin \
        --user=admin \
        --user=kubelet \
        --group=system:serviceaccounts
```

Start Ceph demo container:
```console
$ MON_IP=$(docker exec kube-master route | grep default | awk '{print $2}')
$ CEPH_PUBLIC_NETWORK=${MON_IP}/16
$ docker run -d --net=host -e MON_IP=${MON_IP} \
         -e CEPH_PUBLIC_NETWORK=${CEPH_PUBLIC_NETWORK} \
         -e CEPH_DEMO_UID=foo \
         -e CEPH_DEMO_ACCESS_KEY=foo \
         -e CEPH_DEMO_SECRET_KEY=foo \
         -e CEPH_DEMO_BUCKET=foo \
         -e DEMO_DAEMONS="osd mds" \
         --name ceph_cluster docker.io/ceph/daemon demo
```

Create the pool:
```console
$ docker exec ceph_cluster ceph osd pool create kube 8 8
```

Create the secret:
```console
admin_secret="$(docker exec ceph_cluster ceph auth get-key client.admin)"
kubectl create secret generic csi-rbd-secret \
        --type="kubernetes.io/rbd" \
        --from-literal=admin="${admin_secret}" \
        --from-literal=kubernetes="${admin_secret}"
```

Then we can create k8s objects for storage class, PVC and the pod:
```yaml
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
   name: csi-rbd
   annotations:
       storageclass.kubernetes.io/is-default-class: true
provisioner: csi-rbdplugin
parameters:
    # Comma separated list of Ceph monitors
    # if using FQDN, make sure csi plugin's dns policy is appropriate.
    monitors: 10.192.0.1:6789

    # if "monitors" parameter is not set, driver to get monitors from same
    # secret as admin/user credentials. "monValueFromSecret" provides the
    # key in the secret whose value is the mons
    #monValueFromSecret: "monitors"


    # Ceph pool into which the RBD image shall be created
    pool: kube

    # RBD image format. Defaults to "2".
    imageFormat: "2"

    # RBD image features. Available for imageFormat: "2". CSI RBD currently supports only `layering` feature.
    imageFeatures: layering

    # The secrets have to contain Ceph admin credentials.
    csiProvisionerSecretName: csi-rbd-secret
    csiProvisionerSecretNamespace: default
    csiNodePublishSecretName: csi-rbd-secret
    csiNodePublishSecretNamespace: default

    # Ceph users for operating RBD
    adminid: admin
    userid: admin
    # uncomment the following to use rbd-nbd as mounter on supported nodes
    #mounter: rbd-nbd
reclaimPolicy: Delete
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: rbd-pvc
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: csi-rbd
---
apiVersion: v1
kind: Pod
metadata:
  name: csirbd-demo-pod
spec:
  containers:
   - name: web-server
     image: nginx 
     volumeMounts:
       - name: mypvc
         mountPath: /var/lib/www/html
  volumes:
   - name: mypvc
     persistentVolumeClaim:
       claimName: rbd-pvc
       readOnly: false
```

The pod will start and you'll observe the PV being mounted under
`/var/lib/www/html` inside it (there'll be `lost+found` directory
there).

For snapshots, you can make snapshot class like this:
```yaml
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshotClass
metadata:
  name: csi-snapclass
snapshotter: csi-rbdplugin
parameters:
  monitors: 10.192.0.1:6789
  pool: kube
  imageFormat: "2"
  imageFeatures: layering
  csiSnapshotterSecretName: csi-rbd-secret
  csiSnapshotterSecretNamespace: default
  adminid: admin
  userid: admin
```

Then you can create a snapshot:
```yaml
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshot
metadata:
  name: rbd-pvc-snapshot
spec:
  snapshotClassName: csi-snapclass
  source:
    name: rbd-pvc
    kind: PersistentVolumeClaim
```

The snapshot can be observed to become `ready`:
```console
$ kubectl get volumesnapshot rbd-pvc-snapshot -o yaml
apiVersion: snapshot.storage.k8s.io/v1alpha1
kind: VolumeSnapshot
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"snapshot.storage.k8s.io/v1alpha1","kind":"VolumeSnapshot","metadata":{"annotations":{},"name":"rbd-pvc-snapshot","namespace":"default"},"spec":{"snapshotClassName":"csi-snapclass","source":{"kind":"PersistentVolumeClaim","name":"rbd-pvc"}}}
  creationTimestamp: 2018-10-19T06:07:39Z
  generation: 1
  name: rbd-pvc-snapshot
  namespace: default
  resourceVersion: "7444"
  selfLink: /apis/snapshot.storage.k8s.io/v1alpha1/namespaces/default/volumesnapshots/rbd-pvc-snapshot
  uid: 48337825-d365-11e8-aec0-fae2979a43cc
spec:
  snapshotClassName: csi-snapclass
  snapshotContentName: snapcontent-48337825-d365-11e8-aec0-fae2979a43cc
  source:
    kind: PersistentVolumeClaim
    name: rbd-pvc
status:
  creationTime: 2018-10-19T06:07:40Z
  ready: true
  restoreSize: 1Gi
```

You can also find it using Ceph tools in the `ceph_cluster` container.
You can use `rbd ls --pool=kube` to list the RBD images and then
inspecting the one corresponding to your PV, e.g.
```console
$ rbd snap ls kube/pvc-aff10346d36411e8
```
(if you've created just one PV/PVC, there'll be just one image in the
`kube` pool).

In theory, you should be able to make a new PV from the snapshot like this:
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-restore
spec:
  # storageClassName: csi-rbd
  dataSource:
    name: rbd-pvc-snapshot
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
```

but unfortunately this part didn't work for me. I was getting PVC
modification errors and even commented out `storageClassName: csi-rbd`
didn't help (it did help with similar problems in external-storage
project).

Another problem that I've encountered that when I've attempted to make
a block PVC for Virtlet persistent rootfs
```yaml
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
  volumeMode: Block
  rbd:
    image: tstimg
    monitors:
    - 10.192.0.1:6789
    pool: kube
    secretRef:
      name: ceph-admin
```

the PV got actually created but PVC remained pending because of
another problem with updating PVC. Obviously either
`external-snapshotter`, `ceph-csi` or both projects need some fixes,
but we can hope this functionality will get mature soon (or we can
help fixing it).
