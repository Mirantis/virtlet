# Defining a VM Pod

The basic idea of a VM pod is that it's a plain Kubernetes pod
definition with the following conditions satisfied:

1. It has `kubernetes.io/target-runtime: virtlet.cloud`
   [annotation](#cri-proxy-annotation) so it can be recognized by
   [CRI Proxy](https://github.com/Mirantis/criproxy).
2. The pod has exactly one container.
3. The container's image has `virtlet.cloud/` prefix followed by
   the image name which is recognized according to Virtlet's
   [image handling rules](../images/) and used to download
   the QCOW2 image for the VM.

If you have Virtlet running only on some of the nodes in the cluster,
you also need to specify either `nodeSelector` or `nodeAffinity` for
the pod to have it land on a node with Virtlet. If a VM pod lands on a
node which doesn't have Virtlet or where Virtlet and
[CRI Proxy](https://github.com/Mirantis/criproxy) aren't configured
properly, you can see the following messages in `kubectl describe`
output for the VM pod (the message can be printed as a single line):
```
Warning  Failed     5s (x2 over 17s)  kubelet, kubemaster
Failed to pull image "virtlet.cloud/cirros":
rpc error: code = Unknown desc = Error response from daemon:
Get https://virtlet.cloud/v2/: dial tcp 50.63.202.10:443:
connect: connection refused
```
This means that kubelet is trying to pull the VM image from a Docker
registry.

It's also possible to construct higher-level Kubernetes objects such
as Deployment, StatefulSet or DaemonSet out of VM pods, in which case
the `template` section of the object must follow the above rules for
VM pods.

Below is an example of a Virtlet pod. The comments describe the
particular parts of the pod spec. The following sections will give
more details on each part of the spec.

```yaml
# Standard k8s pod header
apiVersion: v1
kind: Pod
metadata:
  # the name of the pod
  name: cirros-vm
  # See 'Annotations recognized by Virtlet' below
  annotations:
    # This tells CRI Proxy that this pod belongs to Virtlet runtime
    kubernetes.io/target-runtime: virtlet.cloud
    # An optional annotation specifying the count of virtual CPUs.
    # Defaults to "1".
    VirtletVCPUCount: "1"
    # CirrOS doesn't load nocloud data from SCSI CD-ROM for some reason
    VirtletDiskDriver: virtio
    # inject ssh keys via cloud-init
    VirtletSSHKeys: |
      ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCaJEcFDXEK2ZbX0ZLS1EIYFZRbDAcRfuVjpstSc0De8+sV1aiu+dePxdkuDRwqFtCyk6dEZkssjOkBXtri00MECLkir6FcH3kKOJtbJ6vy3uaJc9w1ERo+wyl6SkAh/+JTJkp7QRXj8oylW5E20LsbnA/dIwWzAF51PPwF7A7FtNg9DnwPqMkxFo1Th/buOMKbP5ZA1mmNNtmzbMpMfJATvVyiv3ccsSJKOiyQr6UG+j7sc/7jMVz5Xk34Vd0l8GwcB0334MchHckmqDB142h/NCWTr8oLakDNvkfC1YneAfAO41hDkUbxPtVBG5M/o7P4fxoqiHEX+ZLfRxDtHB53 me@localhost
    # set root volume size
    VirtletRootVolumeSize: 1Gi
spec:
  # This nodeAffinity specification tells Kubernetes to run this
  # pod only on the nodes that have extraRuntime=virtlet label.
  # This label is used by Virtlet DaemonSet to select nodes
  # that must have Virtlet runtime
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
  - name: cirros-vm
    # This specifies the image to use.
    # virtlet.cloud/ prefix is used by CRI proxy, the remaining part
    # of the image name is prepended with https:// and used to download the image
    image: virtlet.cloud/cirros
    imagePullPolicy: IfNotPresent
    # tty and stdin required for `kubectl attach -t` to work
    tty: true
    stdin: true
    resources:
      limits:
        # This memory limit is applied to the libvirt domain definition
        memory: 160Mi
```

# <a name="annotations"></a>Annotations recognized by Virtlet

The annotations can be specified under `annotations` key of the
`metadata` part of the pod spec. Note that the values are always
strings, but they can be parsed in different way by Virtlet (see Type
column).

For boolean keys, the string `"true"` (lowercase) is interpreted as a
true value. All other values are interpreted as false.

Several keys belong to the [Cloud-Init](../cloud-init/) settings which
are described in detail in the
[corresponding section](../cloud-init/).

| Key | Description | Value | Default |
| --- | --- | --- | --- |
| <sub>[kubernetes.io/target-runtime](#cri-proxy-annotation)</sub> | [CRI runtime setting for CRI Proxy](#cri-proxy-annotation) | `virtlet.cloud` | `virtlet.cloud` |
| <sub>[VirtletChown9pfsMounts](../volumes/#9pfs-mounts)</sub> | [Recursively chown 9pfs mounts](../volumes/#9pfs-mounts) | boolean | `""` |
| <sub>[VirtletCloudInitImageType](../cloud-init/##output-iso-image-format)</sub> | [Cloud-Init](../cloud-init/##output-iso-image-format) image type to use | `"nocloud"` `"configdrive"` | `""` |
| <sub>[VirtletCloudInitMetaData](../cloud-init/#detailed-structure-of-the-generated-files)</sub> | The contents of [Cloud-Init](../cloud-init/) metadata | json / yaml | `""` |
| <sub>[VirtletCloudInitUserData](../cloud-init/#detailed-structure-of-the-generated-files)</sub> | The contents of [Cloud-Init](../cloud-init/) user-data (mergeable) | json / yaml | `""` |
| <sub>[VirtletCloudInitUserDataOverwrite](../cloud-init/#detailed-structure-of-the-generated-files)</sub> | Disable merging of [Cloud-Init](../cloud-init/) user-data keys | boolean | `""` |
| <sub>[VirtletCloudInitUserDataScript](../cloud-init/#detailed-structure-of-the-generated-files)</sub> | The contents of [Cloud-Init](../cloud-init/) user-data as a script | text | `""` |
| <sub>[VirtletCloudInitUserDataSource](../cloud-init/#detailed-structure-of-the-generated-files)</sub> | Data source for [Cloud-Init](../cloud-init/) user-data | `"configmap/..."` `"secret/..."` | `""` |
| <sub>[VirtletCloudInitUserDataSourceEncoding](../cloud-init/#propagating-user-data-from-kubernetes-objects)</sub> | Encoding to use for loading [Cloud-Init](../cloud-init/) user-data from a ConfigMap key | `"plain"` | `"base|4"` | `"plain"` |
| <sub>[VirtletCloudInitUserDataSourceKey](../cloud-init/#propagating-user-data-from-kubernetes-objects)</sub> | ConfigMap key to load [Cloud-Init](../cloud-init/) user-data from | | `""` |
| <sub>[VirtletCPUModel](#cpu-model)</sub> | [CPU model to use](#cpu-model) | `""` `"host-model"` | `""` |
| <sub>[VirtletDiskDriver](#disk-driver)</sub> | [Disk driver to use](#disk-driver) | `"scsi"` `"virtio"` | `"scsi"` |
| <sub>[VirtletFilesFromDataSource](#injecting-files-into-the-image)</sub> | Inject files from a ConfigMap or a Secret into the image | `"configmap/..."` `"secret/..."` | `""` |
| <sub>[VirtletLibvirtCPUSetting](#cpu-model)</sub> | libvirt [CPU model](#cpu-model) setting | yaml | `""`
| <sub>[VirtletRootVolumeSize](../volumes/#root-volume-size)</sub> | [Root volume size](../volumes/#root-volume-size) | quantity | `""` |
| <sub>[VirtletSSHKeys](../cloud-init/#detailed-structure-of-the-generated-files)</sub> | SSH keys to add to the VM injected via [Cloud-Init](../cloud-init/) | a list of strings | `""` |
| <sub>[VirtletSSHKeySource](../cloud-init/#detailed-structure-of-the-generated-files)</sub> | Data source for ssh keys injected via [Cloud-Init](../cloud-init/) | `"configmap/..."` `"secret/..."` | `""` |
| <sub>[VirtletVCPUCount](#vcpu-count)</sub> | [The number of vCPUs to assign to the VM pod](#vcpu-count) | integer | `"1"` |

## CRI Proxy annotation

Besides Virtlet annotations, there's `kubernetes.io/target-runtime:
virtlet.cloud` annotation which is handled by
[CRI Proxy](https://github.com/Mirantis/criproxy).  It's important to
specify it as well as `virtlet.cloud` prefix for CRI Proxy to be able
to direct requests to Virtlet.

## Chowning 9pfs mounts

Setting `VirtletChown9pfsMounts` to `true` causes
[9pfs mounts](#9pfs-mounts) to chown their volume contents to make
it readable and writable by the VM.

## CPU Model

`VirtletCPUModel: host-model` annotation enables nested
virtualization.  `VirtletLibvirtCPUSetting` is an expert-only
annotation that sets the CPU options for libvirt. The YAML keys
correspond to the XML elements and attributes in the
[libvirt XML definition](https://libvirt.org/formatdomain.html#elementsCPU),
but are capitalized. The value of `Model` field goes into the `Value`
key. For example:

```yaml
Match: exact
Model:
  Fallback: allow
  Value: core2duo
```

## Disk driver

The driver is set using `VirtletDiskDriver` annotation which may have
the value of `scsi` (the default) or `virtio`.  Some OS images may
have problem with the default `scsi` driver, for example, CirrOS
can't handle [Cloud-Init](../cloud-init/) data unless `virtio` driver
is used.

## Injecting files into the image

By using `VirtletFilesFromDataSource` annotation, it's possible to
place the contents of a ConfigMap or a Secret on the image before
booting the VM. For more information, refer to
[Injecting files into the VM](../injecting-files/).

## vCPU count

Virtlet defaults to using just one vCPU per VM. You can change this
value by setting `VirtletVCPUCount` annotation to the desired value,
for example, `VirtletVCPUCount: "2"`.

# Volume handling

Virtlet can recognize and handle pod's `volumes` and container's
`volumeMounts` sections. This can also be used to make the VM use a
persistent root filesystem which will survive pod removal and
re-creation. For more information on working with volumes, please
refer to the [Volumes](../volumes/) section.

# Environment variables

Virtlet supports passing environment variables to the VM using the standard
`env` settings in the container definition:

```yaml
...
spec:
...
containers:
  - name: cirros-vm
    ...
    env:
    - name: MY_FOO_VAR
      value: foo
    - name: MY_FOOBAR_VAR
      value: foobar
```

Virtlet uses [Cloud-Init](../cloud-init/) mechanisms to write the
values into `/etc/cloud/environment` file inside the VM which
has the same `key=value` per line format as `/etc/environment`
and can be either read by an application or sourced by a shell:
```sh
MY_FOO_VAR=foo
MY_FOOBAR_VAR=foobar
```

For this environment mechanism to work, the cloud-init implementation
inside the VM must be able to handle `write_files` inside the
Cloud-Init user-data.
