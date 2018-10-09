# Tuning rootfs of VM using config map as files

Virtlet provides as an option a way to tune content of rootfs created from
image pointed in `container` part of pod definition by using an user selected
[Config Map](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)
as a source of data.

## Usage

Prepare config map:
```bash
kubectl create configmap sample-data --from-file=/path/to/some/directory
```

then add to pod annotations: `VirtletConfigMapAsFiles: sample-data`.

## Limitations

At the moment there is no support for rootfs tuning and simultanous usage
of persistent block volumes for storing rootfs data in the same time.
