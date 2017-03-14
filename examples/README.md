# Virtlet image server and pod example

In order to try out the example, do the following on a cluster that
has nodes with Virtlet on it:

1. Create image server Deployment and Service:
```
kubectl create -f image-server.yaml -f image-service.yaml
```
2. Wait for image-server pod to become `Running`:
```
kubectl get pods -w
```
3. Create a sample VM:
```
kubectl create -f cirros-vm.yaml
```
4. Wait for `cirros-vm` pod to become `Running`:
```
kubectl get pods -w
```
5. List libvirt domains:
```
./virsh.sh list
```
6. Connect to the VM console:
```
./virsh.sh console $(./virsh.sh list --name)
```
