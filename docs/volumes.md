# Volume Handling

Virtlet volumes can use either `virtio-blk` or `virtio-scsi` storage
backends for the volumes. `virtio-scsi` is the default, but it can be
overridden using `VirtletDiskDriver` annotation, which can have one of
two values: `virtio` meaning `virtio-blk` and `scsi` meaning
`virtio-scsi` (the default). Below is an example of switching a pod
to `virtio-blk` driver:


```yaml
apiVersion: v1
kind: Pod
metadata:
  name: cirros-vm
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
    VirtletDiskDriver: virtio
```

The values of the `VirtletDiskDriver` annotation correspond to values
of `bus` attribute of libvirt disk target specification.

The selected mechanism is used for the rootfs, nocloud cloud-init
CD-ROM and all the flexvolume types that Virtlet supports.

## Caveats and Limitations

1. The overall allowed number of volumes that can be attached to a
   single VM (including implicit volumes for the boot disk and nocloud
   cloud-init CD-ROM) is 20 in case of `virtio-blk` and 26 in case of
   `virtio-scsi` driver. The limits can be exteneded in future.
1. When generating libvirt domain definition, Virtlet constructs disk
   names as ```sd + <disk-char>``` in case of `virtio-scsi` and as
   ```vd + <disk-char>``` in case of `virtio-blk`, where `disk-char`
   is a lowercase latin letter starting with 'a'. The first block
   device, `sda` or `vda`, is used for the boot disk.

```xml
<domain type='qemu' id='2' xmlns:qemu='http://libvirt.org/schemas/domain/qemu/1.0'>
  <name>de0ae972-4154-4f8f-70ff-48335987b5ce-cirros-vm-rbd</name>
....

  <devices>
    <emulator>/vmwrapper</emulator>
    <disk type='file' device='disk'>
      ...
      <target dev='sda' bus='scsi/'>
      ...
    </disk>
    <disk type='file' device='disk'>
      ...
      <target dev='sdb' bus='scsi'/>
      ...
    </disk>
    <disk type='network' device='disk'>
      ...
      <target dev='sdc' bus='scsi'/>
      ...
    </disk>

    ...
    </devices>

...
</domain>
```

Virtlet tries to do its best so as to make the disk names specified in
the domain definition to match the devices inside VM in case of Linux
system.  This will be the case with most Linux images when using
`virtio-scsi` driver, but in case of `virtio-blk` it depends on
whether the device naming/numbering in the VM follows the numbering of
PCI slots used for `virtio-blk` devices. We didn't study this part
well enough yet.

3. The attached disks are visible by the OS inside VM as hard disk
   devices `/dev/sdb`, `/dev/sdc` and so on (`/dev/vdb`, `/dev/vdc`
   and so on in case of `virtio-blk`). As said above there is no fixed
   behavior for device names and their order on the PCI bus.
4. As with the majority of other guest OS functionality, Virtlet
   doesn't give any guarantee about the possibility of mounting
   flexvolumes into the VM. This depends on the cloud-init
   functionality supported by the image used. Also note that Virtlet
   doesn't perform any checks on the guest OS side to ensure that
   volume mounting succeeded.

## Flexvolume driver

Virtlet uses custom
[FlexVolume](https://github.com/kubernetes/community/blob/master/contributors/devel/flexvolume.md)
driver (`virtlet/flexvolume_driver`) to specify block devices for the
VMs. Flexvolume options must include `type` field with one of the
following values:
* `qcow2` - ephemeral volume
* `raw` - raw device
* `ceph` - Ceph RBD

See the following sections for more info on these.

## Ephemeral Local Storage

**Volume naming:** `<domain-uuid>-<vol-name-specified-in-the-flexvolume>`
**Defaults**:
```
          capacity: 1024MB
```

All ephemeral volumes created by request as well as clones of boot images are stored
at local storage libvirt pool "**volumes**" under `/var/lib/virtlet/volumes`.

Volume settings for ephemeral local storage volumes are passed via flexvolume options.

See the following example:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-vm-pod
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: extraRuntime
            operator: In
            values:
            - virtlet
  containers:
    - name: test-vm
      image: download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img
  volumes:
  - name: vol1
    flexVolume:
      driver: "virtlet/flexvolume_driver"
      options:
        type: qcow2
        capacity: 1024MB
  - name: vol2
    flexVolume:
      driver: "virtlet/flexvolume_driver"
      options:
        type: qcow2
```

According to this definition will be created VM-POD with VM with 2
equal volumes, attached, which can be found in "volumes" pool under
`<domain-uuid>-vol1` and `<domain-uuid>-vol2`. A clone of boot image is
exposed as `sda` device to the guest OS.
On a typical Linux system the additional volume disks are assigned to
`/dev/sdX` (`/dev/vdX` in case of `virtio-blk`) devices in an
alphabetical order, so vol1 will be `/dev/sdb` (`/dev/vdb`) and vol2
will be `/dev/sdc` (`/dev/vdc`), but please refer to the caveat #3 at
the beginning of this document.

When a pod is removed, all the volumes related to it are removed
too. This includes the root disk (a clone of the boot image) and any
additional volumes.

## Persistent Storage

Virtlet currently supports attaching Ceph RBDs (RADOS Block Devices) to the VMs.
Cephx authentication can be enabled for the Ceph clusters that are used with this driver.

As of now, there's no need to mount volumes into the container, it's enough to define them for the pod, but this may change in future.

#### Supported features of RBD Volume definition

```
- FlexVolume Driver name: kubernetes.io/flexvolume_driver
- type: ceph
- monitor: <ip:port>
- user: <user-name>
- secret: <user-secret-key>
- volume: <rbd-image-name>
- pool: <pool-name>
```

## Flexvolume driver implementation details
1. It's expected that the driver's binary resides at `/usr/libexec/kubernetes/kubelet-plugins/volume/exec/virtlet~flexvolume_driver/flexvolume_driver` before kubelet is started. Note that if you're using DaemonSet for virtlet deployment, you don't need to bother about that because in that case it's done automatically.
1. Kubelet calls the virtlet flexvolume driver and passes volume info to it
1. Virtlet flexvolume driver uses standard kubelet dir `/var/lib/kubelet/pods/<pod-id>/volumes/virtlet~flexvolume_driver/<volume-name>` to store a JSON file with flexvolume configuration.
4. Virtlet checks whether there are dirs with volume info under `/var/lib/kubelet/pods/<pod-id>/volumes/virtlet~flexvolume_driver`. If yes, virtlet parses the JSON configuration file and updates the domain definition accordingly.

#### Example of VM-pod definition with a ceph volume:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: cirros-vm-rbd
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: extraRuntime
            operator: In
            values:
            - virtlet
  containers:
    - name: cirros-vm-rbd
      image: virtlet.cloud/cirros
  volumes:
    - name: test
      flexVolume:
        driver: kubernetes.io/flexvolume_driver
        options:
          Type: ceph
          Monitor: 10.192.0.1:6789
          User: libvirt
          Secret: AQDTwuVY8rA8HxAAthwOKaQPr0hRc7kCmR/9Qg==
          Volume: rbd-test-image
          Pool: libvirt-pool
```

### Example of VM-pod definition with a ceph volume using [PVs and PVCs](https://kubernetes.io/docs/concepts/storage/persistent-volumes/).

```yaml
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-rbd-virtlet
spec:
  capacity:
    storage: 10M
  accessModes:
    - ReadWriteOnce
  flexVolume:
    driver: "virtlet/flexvolume_driver"
    options:
      type: ceph
      monitor: 10.192.0.1:6789
      user: libvirt
      secret: AQA1VTpZMnf7ChAAqWQPmvq8pIXPYIDBiRsXeA==
      volume: rbd-test-image-pv
      pool: libvirt-pool
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: rbd-claim
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10M
---
apiVersion: v1
kind: Pod
metadata:
  name: cirros-vm-rbd
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: extraRuntime
            operator: In
            values:
            - virtlet
  containers:
    - name: cirros-vm-rbd
      image: virtlet.cloud/cirros
  volumes:
    - name: test
      persistentVolumeClaim:
        claimName: rbd-claim

```

**NOTE: All defined volumes will be attached to VM, no additional settings needed inside container spec.**


```
# virsh domblklist 2
Target     Source
------------------------------------------------
sda        /var/lib/virtlet/virtlet_root_de0ae972-4154-4f8f-70ff-48335987b5ce
sdb        libvirt-pool/rbd-test-image
```

### Raw devices

Volume settings for locally accessible raw devices are passed by adding `raw` flexvolume to a pod.

See the following example:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-vm-pod
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: extraRuntime
            operator: In
            values:
            - virtlet
  containers:
    - name: test-vm
      image: download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img
  volumes:
  - name: raw
    flexVolume:
      driver: "virtlet/flexvolume_driver"
      options:
        type: raw
        # this assumes that some file is associated with /dev/loop0 on
        # the virtlet node using losetup
        path: /dev/loop0
```

As always, the boot disk is exposed to the guest OS as `sda` device.
This pod definition exposes a single raw device to the VM (/dev/loop0).
As devices/volumes are exposed in the alphabet order starting from `b`, `vol1` will be visible on typical Linux VM as `sdb`, but please refer to the caveat #3 at the beginning of this document.

#### Raw device whitelist

Virtlet only allows exposing to VM only those raw devices that are whitelisted. This list is controlled by `-raw-devices` parameter for `virtlet` binary. Its value is passed to `virtlet` daemonset using `VIRTLET_RAW_DEVICES` environment variable.
This `-raw-devices` parameter should contain comma separated patterns of paths relative to `/dev` directory, which are [globbed](https://en.wikipedia.org/wiki/Glob_(programming)) to get the list of paths of raw devices that can be used by virtual machines.
When not set, it defaults to `loop*`.

One way to pass this parameter to `virtlet` is to use [configmap](https://kubernetes.io/docs/tasks/configure-pod-container/configmap) to contain a key/value pair (e.x. `devices.raw=loop*,mapper/vm_pool-*`), which then can be used by modifying `deploy/virtlet-ds.yaml` in the following manner:
```yaml
...
spec:
  ...
  containers:
  - name: virtlet
    ...
    env:
      ...
      - name: VIRTLET_RAW_DEVICES
        valueFrom:
          configMapKeyRef:
            name: name-of-configmap-for-this-node
            key: devices.raw
```

### Mounting the volumes into the VMs

In case if the guest OS supports proper `#cloud-config` format of
cloud-init userdata which includes supporting `mounts` module,
it's possible to mount the flexvolumes into the VM using usual
pod `volumeMounts` notation:

```yaml
...
spec:
...
  containers:
  - name: ubuntu-vm
    image: virtlet.cloud/cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img
    volumeMounts:
    - name: docker
      mountPath: /var/lib/docker
  volumes:
  - name: docker
    flexVolume:
      driver: "virtlet/flexvolume_driver"
      options:
        type: qcow2
        capacity: 2048MB
```

The example above adds an ephemeral volume to the VM and mounts it
under `/var/lib/docker` in the guest OS filesystem.

When mounting a device, Virtlet defaults to mounting its 1st
partition. This can be changed by specifying `part` option in the
flexvolume definition, which may make sense in case of e.g. raw
devices. `part` value of `0` denotes mounting the device itself and
not looking for partitions on it, and `part` values above 0 denote the
corresponding partition numbers. Note that the number must be quoted
so it's treated as a string when parsing yaml. Below is an example:

```yaml
  volumes:
  - name: raw
    flexVolume:
      driver: "virtlet/flexvolume_driver"
      options:
        type: raw
        path: /dev/sdc
        part: "2"
```

## Injecting Secret and ConfigMap content into the VMs as files

Virtlet supports the standard `volumeMounts` notation for placing ConfigMap
and Secret content into the VM filesystem.
It does so using `write_files`
[cloud-init](http://cloudinit.readthedocs.io/en/latest/index.html)
module which is used to write Secret/ConfigMap content to appropriate
locations.

### Consuming a ConfigMap using Kubernetes volume

See [the following example](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#add-configmap-data-to-a-volume)
that demonstrates how to pass ConfigMap data to VM.

[![ConfigMap volume demo](https://asciinema.org/a/084dbr1V0zpxNbF4iaTJpEgJA.png)](https://asciinema.org/a/084dbr1V0zpxNbF4iaTJpEgJA)

### Secret as volume

Secrets can be consumed in the same manner as ConfigMaps, see
[the example](https://kubernetes.io/docs/concepts/configuration/secret/#use-case-pod-with-ssh-keys).

Like any other pod, Virtlet VM pods have predefined secret with Kubernetes API
access token which is written into
`/var/run/secrets/kubernetes.io/serviceaccount` directory.
