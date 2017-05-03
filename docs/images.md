# Image Handling

Virtlet supports qcow2 format images.

## Options:
- `ImagePullSecrets` - not supported currently
- Protocol to use to download the image. By default `https` is used. In order to use `http` set `VIRTLET_DOWNLOAD_PROTOCOL` env var to `http` for virtlet container.

## An example of container definition:

```yaml
  containers:
    - name: test-vm
      image: download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img
```

**Note**, that you need to specify url without `scheme://`. In case you using [instructions](../deploy/README.md) in `deploy/` directory to deploy virtlet, you need to add `virtlet/` prefix to the url.


## Image handling Workflow:
1. Kubelet sends `PullImage` gRPC request to Virtlet.
(Note, that `PullImage` request can be skipped by kubelet unless the pod has `imagePullPolicy: PullAlways` or `imagePullPolicy: PullIfNotPreset` and the image is not pulled yet.)
1. Virtlet uses image url fragment after last slash as internal image name that it looks up in the list of existent images on the host.
1. In case if there's no image with such name in the list, the image will be downloaded using specified url prepended with `scheme://`. After that Virtlet creates libvirt volume in "**default**" libvirt pool under `/var/lib/libvirt/images` and copies the image content to it.
1. In case if there's an entry with such name in list, `PullImage` call just return success..

**Note**, that in order to safe disk space and to be able to reuse existing guest OS volumes Virtlet creates separate volume which uses the original one as backing store.
All snapshot volumes are stored in "**volumes**" libvirt pool under `/var/lib/virtlet/volumes`.
