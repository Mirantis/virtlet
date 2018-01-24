# Image Handling

Virtlet supports QCOW2 format for VM images.

## Options:
- `ImagePullSecrets` - not supported currently
- protocol to use to download the image. By default `https` is
  used. In order to use `http` set `VIRTLET_DOWNLOAD_PROTOCOL` env var
  to `http` for virtlet container. `<scheme>://` below denotes
  the selected protocol

## An example of container definition:

```yaml
  containers:
    - name: test-vm
      image: download.cirros-cloud.net/0.3.5/cirros-0.3.5-x86_64-disk.img
```

**Note:** You need to specify url without `<scheme>://`. In case you are using [instructions](../deploy/README.md) in `deploy/` directory to deploy Virtlet, you need to add `virtlet.cloud/` prefix to the url.

Also see [Image Name Translation](image-name-translation.md) for another way of providing image URL.

Virtlet uses filesystem-based image store for the VM images.
The images are stored like this:

```
/var/lib/virtlet/images
  links/
    example.com%whatever%etc -> ../data/2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881
    example.com%same%image   -> ../data/2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881
    anotherimg               -> ../data/a1fce4363854ff888cff4b8e7875d600c2682390412a8cf79b37d0b11148b0fa
  data/
    2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881
    a1fce4363854ff888cff4b8e7875d600c2682390412a8cf79b37d0b11148b0fa
```

The files are downloaded to `data/`. File names correspond to SHA256
hashes of their content.

The images are pulled upon `PullImage` gRPC request made by kubelet.
Files are named `part_SOME_RANDOM_STRING` while being downloaded.
After the download finishes, SHA256 hash is calculated to be used as
the data file name, and if the file with that name already exists, the
newly downloaded file is removed, otherwise it's renamed to that
SHA256 digest string. In both cases a symbolic link is created with
the name equal to docker image name but with `/` replaced by `%`, with
the link target being the matching data file.

The image store performs GC upon Virtlet startup, which consists of
removing any part_* files and those files in data/ which have no
symlinks leading to them aren't being used by any containers.

The VMs are started from QCOW2 volumes which use the boot images
as backing store files. The images are stored under `/var/lib/libvirt/images/data`.
VM volumes are stored in "**volumes**" libvirt pool under `/var/lib/virtlet/volumes`
during the VM execution time and are automatically garbage collected by Virtlet
after stopping VM pod environment (sandbox).

**Note:**
Virtlet currently ignores image tags, but their meaning may change
in future, so it’s better not to set them for VM pods. If there’s no tag
provided in the image specification kubelet defaults to
`imagePullPolicy: Always`, which means that the image is always
redownloaded when the pod is created. In order to make pod creation
faster and more reliable, we set in examples `imagePullPolicy` to `IfNotPresent`
so a previously downloaded image is reused if there is one in Virtlet’s
image store.

## Restrictions and pitfalls

Image name are a subject to the strict validation rules that normally applied to the docker image names. Thus one cannot
just put arbitrary URL into the image name. In particular, image names cannot have capital letters, colons and some other
characters that are commonly found in the URLs. Using image name with invalid characters is a common reason for VM
creation failure with non-obvious error status.

In order to overcome these limitations, virtlet provides alternate technology called `Image name translation` that allows
to use alias name for the image and define how this alias translates into the URL along with additional transport options
elsewhere. See [Image Name Translation](image-name-translation.md) document for details.
