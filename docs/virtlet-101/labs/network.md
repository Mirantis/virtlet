## Virtlet Networking

Virtlet [Networking docs](../../networking.md)

### One interface

Virtlet, as other CRI implementations, uses [CNI](https://github.com/containernetworking/cni) (Container Network Interface). When only one interface is used then no additional configuration is required.
As you saw in the previous chapter it just works.

### Multiple interfaces

For multiple interfaces Virtlet uses [CNI-Genie](https://github.com/Huawei-PaaS/CNI-Genie). When running `demo.sh` script you have set `MULTI_CNI=1` which tells `kubeadm-dind-cluster` to install CNI-Genie and two network providers: flannel and calico.

Check that all is already installed:

```bash
kubectl -n kube-system get pod
```

You should see genie, flannel and calico-related pods in the output.

See [Virtlet docs](../../multiple-interfaces.md) for more details about multiple interfaces support.

`demo.sh` script configured CNI-Genie to create two network interfaces for each newly created pod. That includes docker pods and Virtlet pods.

Check the ubuntu-vm to see that it has already configured two interfaces. Attach to the VM (log in as testuser/testuser):

```bash
kubectl attach -it ubuntu-vm-with-testuser
```

and list addresses on interfaces:

```bash
ip address
```

See how to specify which networks to use:

```bash
cat examples/ubuntu-multi-cni.yaml
```

See `cni: "calico,flannel"` annotation. It tells CNI-Genie which networks to use. If no `cni` annotation is specified then [default plugin](https://github.com/Huawei-PaaS/CNI-Genie/tree/master/docs/default-plugin) will be used.
In `demo.sh` script default is set also to "calico,flannel":

```bash
docker exec kube-node-1 cat /etc/cni/net.d/00-genie.conf
```
