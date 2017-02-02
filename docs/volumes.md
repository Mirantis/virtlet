# Volumes Handling

## Ephemeral Local Storage

**Volume naming:** `<domain-uuid>-<vol-name-specified-in-annotation>`
**Limitations:** allowed number of volumes can be attached to single VM is up to 20.
**Defaults**:
```
          "Format": "qcow2"
          "Capacity": "1024"
          "CapacityUnit": "MB"
```

Downloaded qcow2 images are stored at local storage libvirt pool "**default**" at path `/var/lib/libvirt/images`

All ephemeral volumes created by request as well as snapshot for boot image are stored
at local storage libvirt pool "**volumes**" at path /var/lib/virtlet/volumes


Volume settings for ephemeral local storage volumes are passed via pod's metadata Annotations.

See the following example:

```
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
spec:
  containers:
    - name: test-vm
      image: download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img
```

According to this definition will be created VM-POD with VM with 2 equal volumes, attached,  which can be found in "volumes" pool under `<domain-uuid>-vol1` and `<domain-uuid>-vol2`
Boot image is exposed to the guest OS under **vda** device.
Additional volume disks are exposed in the alphabet order starting from b, so vol1 will be vdb and vol2 - vdc

On pod remove expected all volumes and snapshot related to VM should be removed.

## Persistent Storage
TODO
