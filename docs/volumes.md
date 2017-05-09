# Volume Handling

## Caveats and Limitations

1. Virtlet uses virtio block device driver.
1. The overal allowed number of volumes can be attached to single VM is up to 20 regardless of ephemeral, persistent and/or raw devices.
1. Virtlet sets name for disks in a form ```vd + <disk-letter>```, where disk letter for disk is set in alphabet order from 'b' to 'u' (20 in overall) while forms domain's xml definition. The first one - 'vda' - is used for boot image.

```
<domain type='qemu' id='2' xmlns:qemu='http://libvirt.org/schemas/domain/qemu/1.0'>
  <name>de0ae972-4154-4f8f-70ff-48335987b5ce-cirros-vm-rbd</name>
....

  <devices>
    <emulator>/vmwrapper</emulator>
    <disk type='file' device='disk'>
      ...
      <target dev='vda' bus='virtio'/>
      ...
    </disk>
    <disk type='file' device='disk'>
      ...
      <target dev='vdb' bus='virtio'/>
      ...
    </disk>
    <disk type='network' device='disk'>
      ...
      <target dev='vdc' bus='virtio'/>
      ...
    </disk>

    ...
    </devices>

...
</domain>
```
Despite of this you must not expect corresponce between name of device within OS and the one which was set in domain's definition, it's up to Oses, so don't rely on that.

From [Libvirt spec](http://libvirt.org/formatdomain.html#elementsDisks):

> **target**
> The target element controls the bus / device under which the disk is exposed to the guest OS. The dev attribute indicates the "logical" device name. The actual device name specified is not guaranteed to map to the device name in the guest OS. Treat it as a device ordering hint

4. Attached disks are seen inside OS as hard disk devices called dev/vdb, dev/vdc and so on. As said above there is no fixed behaviour form device names as well as regarding to the order on PCI bus.

## Ephemeral Local Storage

**Volume naming:** `<domain-uuid>-<vol-name-specified-in-annotation>`
**Defaults**:
```
          "Format": "qcow2"
          "Capacity": "1024"
          "CapacityUnit": "MB"
```

All ephemeral volumes created by request as well as clones for boot image are stored
at local storage libvirt pool "**volumes**" under `/var/lib/virtlet/volumes`.


Volume settings for ephemeral local storage volumes are passed via pod's metadata Annotations.

See the following example:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-vm-pod
  annotations:
    VirtletVolumes: >
      [
        {
          "Name": "vol1",
          "Format": "qcow2",
          "Capacity": "1024",
          "CapacityUnit": "MB"
        },
        {
          "Name": "vol2"
        }
      ]
    kubernetes.io/target-runtime: virtlet
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
```

According to this definition will be created VM-POD with VM with 2 equal volumes, attached, which can be found in "volumes" pool under `<domain-uuid>-vol1` and `<domain-uuid>-vol2`
Boot image is exposed as volume clone to the guest OS under **vda** device.
On a typical linux system the additional volume disks are assigned to /dev/vdX devices in an alphabetical order, so vol1 will be /dev/vdb and vol2 will be /dev/vdc, but please refer to caveat #3 at the beginning of this document.

On pod remove expected all volumes and root disk (clone of boot image) related to VM should be removed.

## Persistent Storage

### Flexvolume driver

FlexVolume virtlet driver supports attaching Ceph RBDs (RADOS Block Devices) and [NoCloud](http://cloudinit.readthedocs.io/en/latest/topics/datasources/nocloud.html) [cloud-init](https://cloudinit.readthedocs.io/en/latest/) data sources VMs.
Cephx authentication can be enabled for the Ceph clusters that are used with this driver.

Virtlet uses [FlexVolume](https://github.com/kubernetes/community/blob/master/contributors/devel/flexvolume.md) mechanism for the volumes to make volume definitions more consistent with volume definitions of non-VM pods and to make it possible to use [PVs and PVCs](https://kubernetes.io/docs/concepts/storage/persistent-volumes/).

As of now, there's no need to mount volumes inside the container, it's enough to define them for the pod, but this may change in future.

#### Using NoCloud Cloud-init data source mechanism

Virtlet currently supports passing static [cloud-init](https://cloudinit.readthedocs.io/en/latest/) data using [NoCloud](http://cloudinit.readthedocs.io/en/latest/topics/datasources/nocloud.html) data sources VMs. In order to do so, you need to define a `flexVolume` based volume for your pod, with `options` containing `type: nocloud`, `metadata` and optional `userdata` fields. The contents of `metadata` and `userdata` fields will be passed as `user-data` and `meta-data` correspondingly to the VM's cloud-init handler. Note that in some cases there can be VM-dependent restrictions, e.g. the image used by [CirrOS example](../examples/cirros-vm.yaml) supplied with Virtlet only supports JSON data in `metadata` field and a script beginning with `#!` in `userdata` field. Below is the relevant fragment of pod definition:

```yaml
  volumes:
  - name: nocloud
    flexVolume:
      driver: "virtlet/flexvolume_driver"
      options:
        type: nocloud
        metadata: |
          {
            "instance-id": "cirros-vm-001",
            "local-hostname": "my-cirros-vm",
            "public-keys": "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCaJEcFDXEK2ZbX0ZLS1EIYFZRbDAcRfuVjpstSc0De8+sV1aiu+dePxdkuDRwqFtCyk6dEZkssjOkBXtri00MECLkir6FcH3kKOJtbJ6vy3uaJc9w1ERo+wyl6SkAh/+JTJkp7QRXj8oylW5E20LsbnA/dIwWzAF51PPwF7A7FtNg9DnwPqMkxFo1Th/buOMKbP5ZA1mmNNtmzbMpMfJATvVyiv3ccsSJKOiyQr6UG+j7sc/7jMVz5Xk34Vd0l8GwcB0334MchHckmqDB142h/NCWTr8oLakDNvkfC1YneAfAO41hDkUbxPtVBG5M/o7P4fxoqiHEX+ZLfRxDtHB53 me@localhost"
          }

        userdata: |
          #!/bin/sh
          echo "Hi there"
```

Here we set hostname for the VM, inject an ssh public key and provide a script that executed by cloud-init. There's helper script named [examples/vmssh.sh](../examples/vmssh.sh) that can be used to access VMs over ssh after providing ssh keys for them (it defaults to using a sample key from examples/ directory).

Virtlet's cloud-init mechanism is not finalized yet and will change in future so as to support dynamic metadata generation and passing metadata via the metadata server along with `NoCloud` datasource.

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

#### Driver implemetation details
1. It's expected that the driver's binary resides at `/usr/libexec/kubernetes/kubelet-plugins/volume/exec/virtlet~flexvolume_driver/flexvolume_driver` before kubelet is started. Note that if you're using DaemonSet for virtlet deployment, you don't need to bother about that because in that case it's done automatically.
1. Kubelet calls the virtlet flexvolume driver and passes volume info to it
1. Virtlet flexvolume driver uses standard kubelet's dir `/var/lib/kubelet/pods/<pod-id>/volumes/virtlet~flexvolume_driver/<volume-name>` to store the xml definitions to be used by virtlet. Virtlet looks for  `disk.xml`, `secret.xml` and `key` files (`secret.xml` and `key` files are used only if you have cephx auth). For `NoCloud` volumes, there's also a `cidata.cd` directory that's used to generate `cidata.iso` image.

See below an example with some details:
```
# ls -l /var/lib/kubelet/pods/d46318cc-1a80-11e7-ac74-02420ac00002/volumes/virtlet~flexvolume_driver/test/
total 12
-rw-r--r-- 1 root root 337 Apr  6 04:23 disk.xml
-rw-r--r-- 1 root root  40 Apr  6 04:23 key
-rw-r--r-- 1 root root 158 Apr  6 04:23 secret.xml

# cd /var/lib/kubelet/pods/d46318cc-1a80-11e7-ac74-02420ac00002/volumes/virtlet~flexvolume_driver/test/
# cat disk.xml

<disk type="network" device="disk">
  <driver name="qemu" type="raw"/>
  <auth username="libvirt">
    <secret type="ceph" uuid="224355aa-eb5f-4356-64fb-7d2d16a6baad"/>
  </auth>
  <source protocol="rbd" name="libvirt-pool/rbd-test-image">
    <host name="10.192.0.1" port="6789"/>
  </source>
  <target dev="%s" bus="virtio"/>
</disk>
#
#
# cat secret.xml

<secret ephemeral='no' private='no'>
  <uuid>224355aa-eb5f-4356-64fb-7d2d16a6baad</uuid>
  <usage type='ceph'>
    <name>libvirt</name>
  </usage>
</secret>
#
#
# cat key
AQDTwuVY8rA8HxAAthwOKaQPr0hRc7kCmR/9Qg==
```
4. Virtlet checks whether there are dirs with volume info under `/var/lib/kubelet/pods/<pod-id>/volumes/virtlet~flexvolume_driver`. If yes, virtlet includes `disk.xml` content inside domain definition and creates a secret entity in libvirt for cephx auth based on provided `secret.xml` and `key` files.

#### Example of VM-pod definition with specidied rbd device to attach:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: cirros-vm-rbd
  annotations:
    kubernetes.io/target-runtime: virtlet
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
      image: virtlet/image-service.kube-system/cirros
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

**NOTE: All defined volumes will be attached to VM, no additional settings needed inside container spec.**

```
# virsh domblklist 2
Target     Source
------------------------------------------------
vda        /var/lib/virtlet/root_de0ae972-4154-4f8f-70ff-48335987b5ce
vdb        libvirt-pool/rbd-test-image
```

### Raw devices

Volume settings for locally accessible raw devices are passed via pod's metadata Annotations, like for [ephemeral volumes](## Ephemeral Local Storage).

See the following example:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-vm-pod
  annotations:
    VirtletVolumes: >
      [
        {
          "Name": "vol1",
          "Format": "rawDevice",
          "Path": "/dev/loop0"
        },
      ]
    kubernetes.io/target-runtime: virtlet
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
```

As always, boot image is exposed to the guest OS under **vda** device.
This pod definition exposes a single raw device to the VM (/dev/loop0).
As devices/volumes are exposed in the alphabet order starting from `b`, `vol1` will be visible on typical linux VM as `vdb`, but please reffer to third caveats of listed at the top of this document.

#### Raw device whitelist

Virtlet allows to expose to VM only whitelisted raw devices. This list is controlled by `-raw-devices` parameter for `virtlet` binary. It's value is passed to `virtlet` daemonset using `VIRTLET_RAW_DEVICES` environment variable.
This `-raw-devices` parameter should contain comma separated patterns of paths relative to `/dev` directory, which are used to [glob](https://en.wikipedia.org/wiki/Glob_(programming)) paths of devices, allowed to use by virtual machines.
When not set, it defaults to `loop*`.

The easiest method of passing this parameter to `virtlet` is to use [configmap](https://kubernetes.io/docs/tasks/configure-pod-container/configmap) to contain a key/value pair (e.x. `devices.raw=loop*,mapper/vm_pool-*`), which then can be used in `deploy/virtlet_ds.yaml` after setting:
```yaml
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
