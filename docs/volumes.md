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
## Flexvolume libvirt driver
**NOTE: Currently, blocked by qemu connectivity issue, see details in #240**

FlexVolume libvirt driver for virtlet supports attaching to VM of Ceph RBD block devices from cluster with cephx auth enabled.

Basing on [FlexVolumes](https://github.com/kubernetes/community/blob/master/contributors/devel/flexvolume.md) for k8s implemeted defined api to have virtlet be alinged with a way of definition of  remote persistent volumes for pods inside pods(https://kubernetes.io/docs/concepts/storage/volumes/#flexvolume) as well as using [PV&PVC](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)

### RBD Volume definition supported features:

```
- FlexVolume Driver name:  kubernetes.io/libvirt_driver
- Monitor: <ip:port>
- User: <user-name>
- Secret: <user-secret-key>
- Volume: <rbd-image-name>
- Pool: <pool-name>
```

### Driver Implemetation details
1. It's expected driver's binary resides at `/usr/libexec/kubernetes/kubelet-plugins/volume/exec/kubernetes.io~libvirt_driver/libvirt_driver` before kubelet is started
Note: If you're using Daemonset for virtlet deployment, you don't need to bother about that.
2. Kubelet calls libvirt driver and passes volume info to it
3. Libvirt driver uses standart kubelet's dir `/var/lib/kubelet/pods/<pod-id>/volumes/kubernetes.io~libvirt_driver/<volume-name>` to store formed xml definitions to be used by virtlet. It's expected to have three files for each volume: disk.xml, secret.xml and key in case of you have cephx auth, ohterwise only disk.xml will be generated.

See below example with details:
```
# ls -l /var/lib/kubelet/pods/d46318cc-1a80-11e7-ac74-02420ac00002/volumes/kubernetes.io~libvirt_driver/test/
total 12
-rw-r--r-- 1 root root 337 Apr  6 04:23 disk.xml
-rw-r--r-- 1 root root  40 Apr  6 04:23 key
-rw-r--r-- 1 root root 158 Apr  6 04:23 secret.xml

# cd /var/lib/kubelet/pods/d46318cc-1a80-11e7-ac74-02420ac00002/volumes/kubernetes.io~libvirt_driver/test/
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
4. Virtlet checks whether there're dirs with volume info under `/var/lib/kubelet/pods/<pod-id>/volumes/kubernetes.io~libvirt_driver`. If there're virtlet integrates disk.xml inside domain's definition and creates secret entity in libvirt for cephx auth basing on provided secret.xml and key.

### Example of VM-pod definition with specidied rbd device to attach:
```
apiVersion: v1
kind: Pod
metadata:
  name: cirros-vm-rbd
  annotations:
    kubernetes.io/target-runtime: virtlet
    scheduler.alpha.kubernetes.io/affinity: >
      {
        "nodeAffinity": {
          "requiredDuringSchedulingIgnoredDuringExecution": {
            "nodeSelectorTerms": [
              {
                "matchExpressions": [
                  {
                    "key": "extraRuntime",
                    "operator": "In",
                    "values": ["virtlet"]
                  }
                ]
              }
            ]
          }
        }
      }
spec:
  containers:
    - name: cirros-vm-rbd
      image: virtlet/image-service.kube-system/cirros
  volumes:
    - name: test
      flexVolume:
        driver: kubernetes.io/libvirt_driver
        options:
          Monitor: 10.192.0.1:6789
          User: libvirt
          Secret: AQDTwuVY8rA8HxAAthwOKaQPr0hRc7kCmR/9Qg==
          Volume: rbd-test-image
          Pool: libvirt-pool
```

**NOTE: There's no support for _MountPoints_ specified for container, i.e. all defined volumes will be attached to VM.**

```
# virsh domblklist 2
Target     Source
------------------------------------------------
vda        /var/lib/virtlet/snapshot_de0ae972-4154-4f8f-70ff-48335987b5ce
vdb        libvirt-pool/rbd-test-image
```

