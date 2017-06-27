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

## Increasing CRI proxy verbosity level after the bootstrap

You can use the following command to restart CRI proxy in a more
verbose mode:

```bash
docker rm -f $(docker ps -q --filter=label=criproxy=true)
docker run -d --privileged \
      -l criproxy=true \
      --restart always \
       --log-opt max-size=100m \
       --name criproxy \
       --net=host \
       --pid=host \
       --uts=host \
       --userns=host \
       mirantis/virtlet \
       nsenter --mount=/proc/1/ns/mnt -- \
       /opt/criproxy/bin/criproxy \
       -v 3 -alsologtostderr \
       -connect docker,virtlet:/var/run/virtlet.sock
```

`-v` option of `criproxy` controls the verbosity here. 0-1 means some
very basic logging during startup and displaying serious errors, 2 is
the same as 1 plus logging of CRI request errors and 3 causes dumping
of actual CRI requests and responses except for `ListPodSandbox`,
`ListContainers` and `ListImages` requests in addition to what's
logged on level 2. Level 4 adds dumping `List*` requests which may
cause the log to grow fast. `--log-opt` docker option controls the
maximum size of the docker log for CRI proxy container.
