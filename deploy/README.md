# Deploying Virtlet

## Deploying Virtlet on a real cluster

See [this document](real-cluster.md) for instructions.

## Deploying Virtlet as a DaemonSet on kubeadm-dind-cluster

The steps described here are performed automatically by
[demo.sh](demo.sh) script.

1. Start [kubeadm-dind-cluster](https://github.com/Mirants/kubeadm-dind-cluster)
   with Kubernetes version 1.6 (you're not required to download it to your home directory):
```
$ wget -O ~/dind-cluster-v1.6.sh https://cdn.rawgit.com/Mirantis/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.6.sh
$ chmod +x ~/dind-cluster-v1.6.sh
$ ~/dind-cluster-v1.6.sh up
$ export PATH="$HOME/.kubeadm-dind-cluster:$PATH"
```
   The cluster script stores appropriate kubectl version in `~/.kubeadm-dind-cluster`.
2. Label a node to accept Virtlet pod:
```
kubectl label node kube-node-1 extraRuntime=virtlet
```
3. Deploy Virtlet DaemonSet (assuming that you have [virtlet-ds.yaml](virtlet-ds.yaml) in the current directory):
```
kubectl create -f virtlet-ds.yaml
```
4. Wait for Virtlet pod to activate:
```
kubectl get pods -w -n kube-system
```
5. Go to `examples/` directory and follow [the instructions](../examples/README.md) from there.

## Removing Virtlet

In order to remove Virtlet, first you need to delete all the VM pods.

You can remove Virtlet DaemonSet with the following command:
```bash
kubectl delete daemonset -R -n kube-system virtlet
```

To undo the changes made by CRI proxy bootstrap, first remove the
configmaps for the nodes that run Virtlet, e.g. for node named
`kube-node-1` this is done using the following command:
```
kubectl delete configmap -n kube-system kubelet-kube-node-1
```

Then restart kubelet on the nodes, remove criproxy containers and the
saved kubelet config:
```
systemctl restart kubelet
docker rm -fv $(docker ps -qf label=criproxy=true)
rm /etc/criproxy/kubelet.conf
```
