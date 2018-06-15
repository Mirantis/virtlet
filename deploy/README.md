# Deploying Virtlet

## Deploying Virtlet on a real cluster

See [this document](real-cluster.md) for instructions.

## Deploying Virtlet as a DaemonSet on kubeadm-dind-cluster

The steps described here are performed automatically by
[demo.sh](demo.sh) script.

1. Start [kubeadm-dind-cluster](https://github.com/kubernetes-sigs/kubeadm-dind-cluster)
   with Kubernetes version 1.9 (you're not required to download it to your home directory):
   ```
   $ wget -O ~/dind-cluster-v1.9.sh https://cdn.rawgit.com/kubernetes-sigs/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.9.sh
   $ chmod +x ~/dind-cluster-v1.9.sh
   $ ~/dind-cluster-v1.9.sh up
   $ export PATH="$HOME/.kubeadm-dind-cluster:$PATH"
   ```
   The cluster script stores appropriate kubectl version in `~/.kubeadm-dind-cluster`.

1. Label a node to accept Virtlet pod:
   ```bash
   kubectl label node kube-node-1 extraRuntime=virtlet
   ```
1. Make several mounts shared inside the dind node that will run Virtlet:
   ```bash
   for p in /dind /dev /boot /sys/fs/cgroup; do docker exec kube-node-1 mount --make-shared $p; done
   ```
1. Add virtlet image translation configmap:
   ```bash
   kubectl create configmap -n kube-system virtlet-image-translations --from-file images.yaml
   ```
1. Install CRI proxy on the node:
   ```bash
   CRIPROXY_DEB_URL="https://github.com/Mirantis/criproxy/releases/download/v0.10.0/criproxy-nodeps_0.10.0_amd64.deb"
   docker exec kube-node-1 /bin/bash -c "curl -sSL '${CRIPROXY_DEB_URL}' >/criproxy.deb && dpkg -i /criproxy.deb && rm /criproxy.deb"
   ```
1. Download `virtletctl` binary for `virtlet` release you need (replace `N.N.N` in the command below accordingly):
   ```bash
   curl -SL -o virtletctl https://github.com/Mirantis/virtlet/releases/download/vN.N.N/virtletctl
   chmod +x virtletctl
   ```
   In case if you're using Mac OS X, you need to use this command instead:
   ```bash
   curl -SL -o virtletctl https://github.com/Mirantis/virtlet/releases/download/vN.N.N/virtletctl.darwin
   chmod +x virtletctl
   ```
   You can also use `virtletctl` from Virtlet image, see below.
1. Deploy Virtlet DaemonSet and related objects:
   ```bash
   ./virtletctl gen | kubectl apply -f -
   ```
   If you want to use the latest image, you can use `virtletctl` from that image:
   ```bash
   docker run --rm mirantis/virtlet:latest virtletctl gen --tag latest | kubectl apply -f -
   ```
   You can also use other image tag instead of `latest`, just replace it in both places in the above command.
1. Wait for Virtlet pod to activate:
   ```bash
   kubectl get pods -w -n kube-system
   ```
1. Go to `examples/` directory and follow [the instructions](../examples/README.md) from there.

## Configuring Virtlet

Virtlet can be customized through the `virtlet-config` ConfigMap
Kuberenetes object.  The following keys in the config map are honored
by Virtlet when it's deployed using k8s yaml produced by `virtletctl gen`:

  * `disable_kvm` - disables KVM support and forces QEMU instead. Use "1" as a value.
  * `download_protocol` - default image download protocol - either `http` or `https`. The default is https.
  * `loglevel` - integer log level value for the virtlet written as a string (e.g. "3", "2", "1").
  * `calico-subnet` - netmask width for the Calico CNI. Default is "24".
  * `image_regexp_translation` - enables regexp syntax for the image name translation rules.
  * `disable_logging` - disables log streaming from VMs. Use "1" to disable.

It's also possible to set [per-node configuration](../docs/config.md)
for Virtlet using CRDs.

## Removing Virtlet

In order to remove Virtlet, first you need to delete all the VM pods.

You can remove Virtlet DaemonSet with the following command:
```bash
kubectl delete daemonset -R -n kube-system virtlet
```

After that you can remove CRI Proxy if you're not going to use the
node for Virtlet again by undoing the steps you made to install it
(see CRI Proxy
[documentation](https://github.com/Mirantis/criproxy/)).
