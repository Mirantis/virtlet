# Image Handling

Virtlet supports qcow2 format images for VM boot images.

## Options:
- ImagePullSecrets - not supported currently
- Protocol to use to download the image. By default `https` is used. To set other change `VIRTLET_DOWNLOAD_PROTOCOL` env var in `deploy/virtlet-ds.yaml`.

## Example of definition:

```yaml
  containers:
    - name: test-vm
      image: download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img
```

**Note**, that to pass apiserver pod's definition validation, you need to specify url without `scheme://`.


## Image handling Workflow:
1. Kubelet sends PullImage gRPC request to virtlet
(Note, that as ImagePullPolicy value in not included within kubeapi.PullImageRequest to CRI runtime, it's up to kubelet basing on ImagePullPolicy whether to send PullImage gRPC request. So PullImage will be sent in two cases: ImagePullPolicy=PullAlways OR PullIfNotPreset).
1. Virtlet uses image url fragment after last slash as image name to look up though the list of existent images on the host.
1. In case if there's no image with such name in the list, the image will be downloaded using specified url prepended with `scheme://`. And after that Virtlet creates libvirt volume in "**default**" libvirt pool under  `/var/lib/libvirt/images` and uploads image content to it.
1. In case if there's an entry with such name in list, just send succesfull response to kubelet.

**Note**, that to safe disk space and be able to reuse already existent guest OS volumes Virtlet creates separate volume which uses original one as backing store.
All snapshot volumes are stored in "**volumes**" libvirt pool under `/var/lib/virtlet/volumes`.
