# Volumes

[Volumes Documentation](https://github.com/Mirantis/virtlet/blob/master/docs/volumes.md)

Virtlet supports Kubernetes Volumes in a several ways:

## Directory volume with 9pfs

It allows Virtlet to use for example `emptyDir` or `hostPath`. It is a network protocol used over a virtual pci device (does not use networking stack) so when comparing to other options it may have worse performance.


## Persistent Block Volumes

Virtlet can attach local block volume. Ceph based volumes can also be used.

### Persistent Rootfs

Virtlet also supports booting VM from a Persisten Block Volume.

## FlexVolumes

It’s possible to use Virtlet’s flexvolume driver to specify mounting of local block devices, “ephemeral volumes” with their lifetime bound to the one of the pod, and Ceph volumes that are specified as block devices.

See how to use flexvolume with VM Pod:

```bash
cat examples/ubuntu-vm-with-volume.yaml
kubectl create -f examples/ubuntu-vm-with-volume.yaml
```

There are also plans to work on [CSI](https://kubernetes.io/blog/2018/01/introducing-container-storage-interface/)
