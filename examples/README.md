# Virtlet pod example

In order to try out the example, do the following on a cluster that
has nodes with Virtlet on it (see [the instructions](../deploy/README.md) in
`deploy/` directory):

1. Create a sample VM:
```
kubectl create -f cirros-vm.yaml
```
2. Wait for `cirros-vm` pod to become `Running`:
```
kubectl get pods -w
```
3. Connect to the VM console:
```
kubectl attach -it cirros-vm
```
4. As soon as the VM has booted, you can use the `vmssh.sh` script to access it using ssh:
```
./vmssh.sh cirros@cirros-vm [command...]
```

Besides [cirros-vm.yaml](cirros-vm.yaml), there's also [ubuntu-vm.yaml](ubuntu-vm.yaml) that can be used to start an Ubuntu Xenial VM. It can also be accessed using `vmssh.sh` after it boots:
```
./vmssh.sh root@ubuntu-vm [command...]
```

Another way to access a vm is to use `virsh.sh` tool. It can be used to access a vm only when logging is disabled. See [vm-logging](../docs/devel/vm-logging.md) for more details.
You can list libvirt domains with `virsh.sh`:
```bash
./virsh.sh list
```

And then connect to console:

```
$ ./virsh.sh console @cirros-vm
Connected to domain 411c70b0-1df3-46be-4838-c85474a1b44a-cirros-vm
Escape character is ^]
login as 'cirros' user. default password: 'cubswin:)'. use 'sudo' for root.
cirros-vm login: cirros
Password:
$
```


# Kubernetes on VM-based StatefulSet

[Another example](k8s.yaml) involves starting several VMs using `StatefulSet` and deploying
Kubernetes using `kubeadm` on it.

You can create the cluster like this:
```
kubectl create -f k8s.yaml
```

Watch progress of the cluster setup via the VM console:
```
kubectl attach k8s-0
```

After it's complete you can log into the master node:

```
./vmssh.sh k8s-0
```

There you can wait a bit for k8s nodes and pods to become ready.
You can list them using the following commands inside the VM:

```
kubectl get nodes -w
kubectl get pods --all-namespaces -o wide -w
```

After that you can follow
[the instructions](../deploy/real-cluster.md) to install Virtlet on
the cluster, but note that you'll have to disable KVM because nested
virtualization is not yet supported by Virtlet.
