# Deploying virtlet as a DaemonSet

The steps described here are performed automatically by
[demo.sh](demo.sh) script.

1. Start [kubeadm-dind-cluster](https://github.com/Mirants/kubeadm-dind-cluster)
   with Kubernetes version 1.5 (you're not required to download it to your home directory):
```
$ wget -O ~/dind-cluster-v1.5.sh https://cdn.rawgit.com/Mirantis/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.5.sh
$ chmod +x ~/dind-cluster-v1.5.sh
$ ~/dind-cluster-v1.5.sh up
$ export PATH="$HOME/.kubeadm-dind-cluster:$PATH"
```
   The cluster script stores appropriate kubectl version in `~/.kubeadm-dind-cluster`.
2. Label a node to accept Virtlet pod:
```
kubectl label node kube-node-1 extraRuntime=virtlet
```
3. Deploy Virtlet DaemonSet:
```
kubectl create -f virtlet-ds.yaml
```
4. Wait for Virtlet pod to activate:
```
kubectl get pods -w -n kube-system
```
5. Go to `examples/` directory and follow [the instructions](../examples/README.md) from there.
