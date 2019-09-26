# Using volumes

Virtlet can recognize and handle pod's `volumes` and container's
`volumeMounts` / `volumeDevices` sections. These can be used to mount
Kubernetes volumes into the VM, as well as attaching block volumes to
the VM and specifying a persistent root filesystem for a VM.

## Consuming raw block PVs

Virtlet supports consuming
[Raw Block Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#raw-block-volume-support)
in the VMs. In order to do this, you need a PVC with `volumeMode:
Block` (let's say its name is `testpvc`) bound to a PV (which needs
also to be `volumeMode: Block`). You can use this mechanism with both
local and non-local PVs.

You can then add the following to pod's volumes:
```yaml
volumes:
- name: testpvc
  persistentVolumeClaim:
    claimName: local-block-pvc
```
and corresponding `volumeDevices` entry to the container:
```yaml
volumeDevices:
- devicePath: /dev/testpvc
  name: testpvc
```

Virtlet will ensure that `/dev/testpvc` inside the VM is a symlink
pointing to the device that corresponds to the block volume (for more
details on this, see [Cloud-Init](../cloud-init/) description.

You can also mount the block device inside the VM using cloud-init:
```yaml
VirtletCloudInitUserData: |
  mounts:
  - ["/dev/testpvc", "/mnt"]
```

See also [block PV examples](https://github.com/Mirantis/virtlet/tree/master/examples#using-local-block-pvs).

## Persistent root filesystem

Although initially Virtlet was only supporting "cattle" VMs that had
their lifespan limited to the one of the pod, it's now possible to
have VMs with persistent root filesystem that survives pod removal
and re-creation, too.

If a persistent block volume is specified for a pod and listed in
container's `volumeDevices` with `devicePath` of `/`:
```yaml
volumeDevices:
- devicePath: /
  name: testpvc
```
the corresponding PV will be used as a persistent root filesystem for
a pod. The persistent root filesystem is reused as long as the image
SHA256 hash doesn't change. Upon the change of SHA256 hash of the VM
image, the PV will be overwritten again. Internally, Virtlet uses
sector 0 of the block device to store persistent root filesystem
metadata, and the block device visible inside the VM will use
the sectors starting from sector 1. Overall, the following algorithm
is used:
1. The block device is checked for the presence of Virtlet header.
2. If there's no Virtlet header, a new header is written to the sector
   0 and the device is overwritten with the contents of the image.
3. If the header contains a future persistent root filesystem metadata
   version number, an error is logged and container creation fails.
4. If the header contains mismatching image SHA256 hash, a new header
   is written to the sector 0 and the device is overwritten with the
   contents of the image.

Unless this algorithm fails on step 3, the VM is booted using the
block PV starting from sector 1 as it's boot device.

*IMPORTANT NOTE:* in case if persistent root filesystem is used,
cloud-init based network setup is disabled for the VM. This is done
because some cloud-init implementations only apply cloud-init network
configuration once, but the IP address given to the VM may change if
the persistent root filesystem is reused by another pod.

See also [block PV examples](https://github.com/Mirantis/virtlet/tree/master/examples#using-the-persistent-root-filesystem).

## Consuming ConfigMaps and Secrets

If a Secret or ConfigMap volume is specified for a Virtlet pod, its
contents is written to the filesystem of the VM using `write_files`
[Cloud-Init](../cloud-init/) feature which needs to be supported by
the VM's Cloud-Init implementation.

## 9pfs mounts

Specifying `volumeMounts` with volumes that don't refer to either
Secrets, ConfigMaps, block PVs or Virtlet-specific flexvolumes causes
Virtlet to mount them using QEMU's VirtFS (9pfs). Note that this means
that the performance may be suboptimal in some cases. File permissions
can also constitute a problem here; you can set
`VirtletChown9pfsMounts` pod annotation to `true` to make Virtlet
change the owner user/group on the directory recursively to one
enabling read-write access for the VM.

## Using FlexVolumes

Virtlet uses custom
[FlexVolume](https://kubernetes.io/docs/concepts/storage/volumes/#flexvolume)
driver (`virtlet/flexvolume_driver`) to specify block devices for the
VMs. Flexvolume options must include `type` field with one of the
following values:

* `qcow2` - ephemeral volume
* `raw` - raw device. This flexvolume type is deprecated in favor of
  Kubernetes' local PVs consumed in [BlockVolume mode](#consuming-raw-block-pvs).
* `ceph` - Ceph RBD. This flexvolume type is deprecated in favor of
  Kubernetes' RBD PVs consumed in [BlockVolume mode](#consuming-raw-block-pvs).

## Ephemeral Local Storage

All ephemeral volumes created by request as well as VM root volumes
are stored in the local libvirt storage pool "**volumes**" which is
located at `/var/lib/virtlet/volumes`. The libvirt volume is named
using the following scheme:
`<domain-uuid>-<vol-name-specified-in-the-flexvolume>`.  The
flexvolume has `capacity` option which specifies the size of the
ephemeral volume and default to 1024 MB.

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
      image: download.cirros-cloud.net/0.3.5/cirros-0.3.5-x86_64-disk.img
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
`<domain-uuid>-vol1` and `<domain-uuid>-vol2`. The root volume, which
uses the VM's QCOW2 image as its backing file, is exposed as `sda`
device to the guest OS.  On a typical Linux system the additional
volume disks are assigned to `/dev/sdX` (`/dev/vdX` in case of
`virtio-blk`) devices in an alphabetical order, so vol1 will be
`/dev/sdb` (`/dev/vdb`) and vol2 will be `/dev/sdc` (`/dev/vdc`), but
please refer to the caveat #3 at the beginning of this document.

When a pod is removed, all the volumes related to it are removed
too. This includes the root volume and any additional volumes.

## Root volume size

You can set the size of the root volume of a Virtlet VM by using
`VirtletRootVolumeSize` annotation. The specified size must be
greater than the QCOW2 volume size, otherwise it will be ignored.
Here's an example:
```yaml
metadata:
  name: my-vm
  annotations:
    kubernetes.io/target-runtime: virtlet.cloud
    VirtletRootVolumeSize: 4Gi
```
This sets the root volume size to 4 GiB unless QCOW2 image size is
larger than 4 GiB, in which case the QCOW2 volume size is used.
The annotation uses the standard Kubernetes quantity specification
format, for more info, see [here](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#meaning-of-memory).

## Disk drivers

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

Most of the time setting the driver is not necessary, but some OS
images may have problem with the default `scsi` driver, for example,
CirrOS can't handle [Cloud-Init](../cloud-init/) data unless `virtio`
driver is used.

## Caveats and limitations

1. The total allowed number of volumes that can be attached to a
   single VM (including implicit volumes for the boot disk and nocloud
   cloud-init CD-ROM) is 20 in case of `virtio-blk` and 26 in case of
   `virtio-scsi` driver. The limits can be extended in future.
2. When generating libvirt domain definition, Virtlet constructs disk
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

3. The attached disks are visible by the OS inside VM as hard disk
   devices `/dev/sdb`, `/dev/sdc` and so on (`/dev/vdb`, `/dev/vdc`
   and so on in case of `virtio-blk`). Note that the naming of the
   devices inside guest OS is usually unpredictable.  The use of
   Virtlet-generated [Cloud-Init](../cloud-init/) data is recommended
   for mounting of the volumes. Virtlet uses udev-provided
   `/dev/disk/by-path/...` or, failing that, sysfs information for
   finding the device inside the virtual machine. Note that both
   mechanisms are Linux-specific.
