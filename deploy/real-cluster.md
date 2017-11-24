# Installing Virtlet on a real cluster

For Virtlet to work, the following prerequisites have to be fulfilled
on the nodes which will run them:

1. Node names must be resolvable via DNS configured on the nodes
1. AppArmor and SELinux must be disabled on the nodes

Virtlet deployment consists of preparing the nodes and then deploying
the Virtlet DaemonSet.

# Installing CRI Proxy

Virtlet requires [CRI Proxy](https://github.com/Mirantis/criproxy)
package to be able to run as DaemonSet on the nodes and support
runnings system pods like `kube-proxy` there. To install CRI Proxy,
please follow the steps from its
[documentation](https://github.com/Mirantis/criproxy/blob/master/README.md).
Repeat it on each node that's going to run Virtlet.

# Deploying Virtlet DaemonSet

First, you need to apply `extraRuntime=virtlet` label to each node that will run Virtlet DaemonSet (replace `XXXXXX` with the node name):
```bash
kubectl label node XXXXXX extraRuntime=virtlet
```

Then you need to install image translations configmap. You can use the default one:
```bash
curl https://raw.githubusercontent.com/Mirantis/virtlet/master/deploy/images.yaml >images.yaml
kubectl create configmap -n kube-system virtlet-image-translations --from-file images.yaml
```

Then you can deploy Virtlet DaemonSet:
```bash
kubectl apply -f https://raw.githubusercontent.com/Mirantis/virtlet/master/deploy/virtlet-ds.yaml
```

By default it has KVM enabled, but you can configure Virtlet to
disable it.  In order to do so, create a configmap named
`virtlet-config` in `kube-system` prior to creating Virtlet DaemonSet
that contains key-value pair `disable_kvm=y`:
```bash
kubectl create configmap -n kube-system virtlet-config --from-literal=disable_kvm=y
```

After completing this step, you can look at the list of pods to see
when Virtlet DaemonSet is ready:
```bash
kubectl get pods --all-namespaces -o wide -w
```

## Testing the installation

There's a couple of scripts that you can use to access the VM. You can
download them from Virtlet repository along with test ssh key:
```
wget https://raw.githubusercontent.com/Mirantis/virtlet/master/examples/{virsh.sh,vmssh.sh,vmkey}
chmod +x virsh.sh vmssh.sh
chmod 600 vmkey
```

Both utilities need `kubectl` to be configured to access your cluster.

`virsh.sh` can be used to access a VM console. `virsh.sh` currently assumes
single Virtlet node per cluster, which will be fixed soon. It supports
convenience notation `@podname[:namespace]` that can be used to refer
to libvirt domain that corresponds to the pod. It also supports additional
command `./virsh.sh poddomain @podname[:namespace]` that displays libvirt
domain id for a pod.
`vmssh.sh` provides ssh access to VM pods.

To test your Virtlet installation, start a sample VM:
```bash
kubectl create -f https://raw.githubusercontent.com/Mirantis/virtlet/master/examples/cirros-vm.yaml
kubectl get pods --all-namespaces -o wide -w
```

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

You can also ssh into the VM:

```
./vmssh.sh cirros@cirros-vm
```

## Removing Virtlet

In order to remove Virtlet, first you need to delete all the VM pods.

You can remove Virtlet DaemonSet with the following command:
```bash
kubectl delete daemonset -R -n kube-system virtlet
```

After doing this, remove CRI proxy from each node by reverting the
changes in Kubelet flags, e.g. by removing
`/etc/systemd/system/kubelet.service.d/20-virtlet.conf` in case of
kubeadm scenario described above. After this you need to restart
kubelet and remove the CRI Proxy binary (`/usr/local/bin/criproxy`)
and its node configuration file (`/etc/criproxy/node.conf`).
