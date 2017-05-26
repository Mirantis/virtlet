# VM Logging
Virtlet runs many VMs on Kubernetes and each of them writes something to `stdout` and `stderr`. User
is given two options:

1. redirect `stdout`/`stderr` into file (DEFAULT)
2. redirect `stdout`/`stderr` into pty console

## Redirecting stdout and stderr into file
By default VM logs are redirected into following file on **node** where Virtlet is running
(node `kube-node-1` when using DIND cluster):
```
/var/log/virtlet/vms/<sandboxId>/raw.log
```
You can verify that this option is enabled by examining `deploy/virtlet-ds.yaml` blueprint. There is
an option that should be set to non-empty string:
```
- name: VIRTLET_VM_LOG_LOCATION
  value: "/var/log/vms"
```

### Deploying virtlet-log DaemonSet
Kubernetes is not able to find and understand raw log files that contain direct dumps from VM. Therefore
we provide an auxilary container `mirantis/virtlet-log` that must be run as a DaemonSet on each node
where Virtlet DaemonSet is running. The purpose of this container, whose blueprint is provided in
`deploy/virtlet-log-ds.yaml`, is reformatting VM logs into a special JSON format that is understood
by Kubernetes.

Provided that:

* redirecting `stdout` and `stderr` into file is turned on
* `deploy/virtlet-log-ds.yaml` DaemonSet is deployed and running

user should be able to both see logs on Kubernetes Dashboard:

IMAGE

Also, obtaining logs from CLI should work:

```bash
$ kubectl logs <MY-POD>
```

### Limitations
There are some limitations when redirecting logs into files is enabled:

- command `virsh console <vm>` works no more since libvirt serial port type is 'file' and not 'pty'
- logs can appear with up to 10 second delay for new VMs since virtlet-log container checks for new
  raw logs every 10 seconds

## Redirecting stdout and stderr into pty
In some cases it may be desired to enable `virsh console <VM>` command that is disabled in case when
redirecting everything into files is enabled (see **Limitations** above). Just use the following setting
in `deploy/virtlet-ds.yaml` blueprint:

```
- name: VIRTLET_VM_LOG_LOCATION
  value: "pty"
```
You can also undeploy `depoly/virtlet-log-ds.yaml` since it won't have any raw logs available to parse,
but nothing will crash if you let it run.

### Limitations
There are some limitations when redirecting logs into files is disabled:

- command `kubectl logs <MY-POD>` will not work
- Kubernetes Dashboard will not display any logs for Virtlet pods

