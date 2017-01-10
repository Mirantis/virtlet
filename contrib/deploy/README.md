# Deploying virtlet as a DaemonSet

1. Start [kubeadm-dind-cluster](https://github.com/Mirants/kubeadm-dind-cluster) on [this k8s fork](https://github.com/ivan4th/kubernetes/tree/mixed-container-runtime-mode)
2. 'Virtletify' `kube-node-1`:
```
./virtletify-dind-node.sh
```
3. Create image server Deployment and Service:
```
kubectl create -f image-server.yaml
kubectl create -f image-service.yaml
```
4. Wait for image-server pod to become Running (this is important for virtlet initialization due to host network + cluster DNS [issue](https://github.com/kubernetes/kubernetes/issues/17406)):
```
kubectl get pods -w
```
5. Create Virtlet DaemonSet:
```
kubectl create -f virtlet-ds.yaml
```
6. Wait for Virtlet to start:
```
kubectl get pods -w
```
7. List libvirt domains:
```
./virsh.sh list
```
8. Connect to the VM console:
```
./virsh.sh console $(./virsh.sh list --name)
```

Notes:

1. Currently CRI proxy doesn't survive node restart (need to add proper systemd unit to fix this).
2. Trying to do `kubectl exec` in virtlet container may currently fail with unhelpful error message ("Error from server"). This is worked around in `virsh.sh` using plain `docker exec`.
