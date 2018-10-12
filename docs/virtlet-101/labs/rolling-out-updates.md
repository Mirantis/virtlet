# Rolling out Updates
## Virtlet Updates

To update Virtlet to the newer or older version just change its image name/tag in DaemonSet definition:

```bash
kubectl -n kube-system edit ds virtlet
```

You should changed in the 4 places there.

During the upgrade state of VM pods will change to `ContainerCreating` but there is nothing to worry. VMs are still running. Kubelet just can not get state of the VMs because Virtlet is not available during update.

When Virtlet update is done check that VMs are still running. Attach to `cirros-vm`:

```bash
kubectl attach -it cirros-vm
```

and run `uptime` command.

Virtlet updates are done using standard [DaemonSet update mechanism](https://kubernetes.io/docs/tasks/manage-daemon/update-daemon-set/)
