# Creating and managing virtual machines

The `demo.sh` script which you've run above has set up Virtlet, started virtual machine with cirros image, started a nginx pod which is exposed via Service.
Let's check how it all works.

The script will ssh into a `cirros-vm` before finishing. Switch to another console and list the pods:


```bash
kubectl get pod
```

`cirros-vm` and `nginx` pod are both listed. Now list the services:

```bash
kubectl get svc
```

Now go back to the first console where you are logged in the `cirros-vm`. Check that you have internet access:

```bash
ping 8.8.8.8
ping mirantis.com
```

Make sure that the cluster network is also accessible from within the pod. Retrieve some data:

```bash
curl <nginx_service_ip>
```

Disconnect from the VM using Ctrl-D.

## View Pod details

Use the `kubectl get` and `kubectl describe` commands to view details for the `cirros-vm` pod:

## View the logs of a Pod

Use the `kubectl logs` command to view the logs for the `cirros-vm` pod:

```
kubectl logs cirros-vm
```

## Attach to the VM

Use the `kubectl attach` command to attach to the VM console:

```
kubectl attach -it cirros-vm
```

## Create new Ubuntu VM

Now you can create another virtual machine. Let's create Ubuntu VM from Virtlet examples:

```bash
cat examples/ubuntu-vm-with-testuser.yaml
kubectl apply -f examples/ubuntu-vm-with-testuser.yaml
```

This example also shows how cloud-init script can be used. Here new user: `testuser` is created and also an ssh key is injected.
Wait for ubuntu VM:

```bash
kubectl get pod -w
```

Now attach to the VM using testuser/testuser credentials:

```bash
kubectl attach -it ubuntu-vm-with-testuser
```

Check if it also has internet connection using the same method which was used for `cirros-vm` above.

## Use in Deployment

Because Virtlet VMs are just normal pods you can create Deployment, DaemonSet or even StatefulSet from Virtual Machines:

```bash
cat examples/deploy-cirros-vm.yaml
kubectl apply -f examples/deploy-cirros-vm.yaml
```

When it's ready you can scale it:

```bash
kubectl scale --replicas=2 deploy/cirros
```

