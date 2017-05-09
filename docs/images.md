# Image Handling

Virtlet supports QCOW2 format for VM images.

## Options:
- `ImagePullSecrets` - not supported currently
- Protocol to use to download the image. By default `https` is used. In order to use `http` set `VIRTLET_DOWNLOAD_PROTOCOL` env var to `http` for virtlet container.

## An example of container definition:

```yaml
  containers:
    - name: test-vm
      image: download.cirros-cloud.net/0.3.4/cirros-0.3.4-x86_64-disk.img
```

**Note:** You need to specify url without `scheme://`. In case you are using [instructions](../deploy/README.md) in `deploy/` directory to deploy Virtlet, you need to add `virtlet/` prefix to the url.


## Image handling Workflow:
1. Kubelet sends `PullImage` gRPC request to Virtlet.
(Note, that `PullImage` request can be skipped by kubelet unless the pod has `imagePullPolicy: PullAlways` or `imagePullPolicy: PullIfNotPreset` and the image is not pulled yet.)
1. Virtlet uses image url fragment after last slash as internal image name that it looks up in the list of existent images on the host.
1. The image will be downloaded using specified url prepended with `scheme://`. After that Virtlet creates libvirt volume in "**default**" libvirt pool under `/var/lib/libvirt/images` and copies the image content to it.

**Note:** Virtual machines are started from volumes which are clones of boot images. They are stored 
in "**volumes**" libvirt pool under `/var/lib/virtlet/volumes` during the VM execution time and are
automatically garbage collected after stopping VM POD environment (sandbox).
