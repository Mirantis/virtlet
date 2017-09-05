# Installing Virtlet on a real cluster

For Virtlet and CRI Proxy to work, the following prerequisites have to
be fulfilled on the nodes which will run them:

1. Node names must be resolvable via DNS configured on the nodes
1. AppArmor and SELinux must be disabled on the nodes

Virtlet deployment consists of preparing the nodes and then deploying
the Virtlet DaemonSet.

The DaemonSet defaults to using CRI Proxy bootstrap, but this mode is
more suited for quick testing of Virtlet and it currently requires
relaxing Kubelet security.

An alternative is preparing the nodes manually by deploying CRI Proxy
there (this of course can also be done using a configuration
management system). In case of manual CRI Proxy setup Virtlet
DaemonSet deployment is done the same way as in case of the bootstrap.

## Preparing the nodes (bootstrap mode)

In order to use CRI Proxy bootstrap, you need to disable
authentication for Kubelet server and enable `DynamicKubeletConfig`
feature gate. For example, if you're using `kubeadm`, you can do this
by creating a file named `/etc/systemd/system/kubelet.service.d/20-virtlet.conf` with the
following content:

```ini
[Service]
Environment="KUBELET_EXTRA_ARGS=--feature-gates=DynamicKubeletConfig=true"
Environment="KUBELET_AUTHZ_ARGS="
```

Then you need to restart kubelet:
```bash
systemctl daemon-reload
systemctl restart kubelet
```

In other cases this may involve specifying `--anonymous-auth` and
removing `--authorization-mode` option from the kubelet command line.

You may want to make sure that kubelet server is accessible using
this command on the node:
```bash
curl -k https://127.0.0.1:10250/configz
```

## Preparing the nodes (manual mode)

The process described here must be repeated on each node that will run Virtlet.

In order to set up Virtlet on a node, first of all you need to get the CRI Proxy binary:
```bash
docker run --rm mirantis/virtlet tar -c /criproxy | tar -C /usr/local/bin -xv
ln -s criproxy /usr/local/bin/dockershim
```

Then you need to create an empty file named `/etc/criproxy/node.conf`
that will prevent virtlet daemonset from invoking the automatic
bootstrap procedure:
```bash
mkdir /etc/criproxy
touch /etc/criproxy/node.conf
```

Now you need to create systemd units that will start the `dockershim`
and CRI Proxy. Here we assume that kubelet is started via
`kubelet.service` systemd unit.

`dockershim` is a part of kubelet that interfaces with Docker. It's
built into `criproxy` binary and is run when `criproxy` binary is
invoked using a symbolic link named `dockershim`. You need to pass
copy the following flags from kubelet command line to `dockershim`:
* `--network-plugin`
* `--hairpin-mode`
* `--non-masquerade-cidr`
* `--cni-conf-dir`
* `--cni-bin-dir`
* `--docker-endpoint`
* `--runtime-request-timeout`
* `--image-pull-progress-deadline`
* `--streaming-connection-idle-timeout`
* `--docker-exec-handler`
* `--seccomp-profile-root`
* `--pod-infra-container-image`
* `--runtime-cgroups`
* `--cgroup-driver`
* `--network-plugin-mtu`
Any other flags are ignored, so you can just copy the whole kubelet
command line there except for kubelet executable itself.

Create a file named `/etc/systemd/system/dockershim.service` with
the following content (you can also use `systemctl --force edit dockershim.service` for it),
replacing `......` with kubelet command line arguments (a naive way to get them
is just to do `ps aux|grep kubelet` if you have `kubelet` service running):

```ini
[Unit]
Description=dockershim for criproxy

[Service]
ExecStart=/usr/local/bin/dockershim ......
Restart=always
StartLimitInterval=0
RestartSec=10

[Install]
RequiredBy=criproxy.service
```

Create a file named `/etc/systemd/system/criproxy.service` with
the following content (you can also use `systemctl --force edit criproxy.service` for it):

```ini
[Unit]
Description=CRI Proxy

[Service]
ExecStart=/usr/local/bin/criproxy -v 3 -alsologtostderr -connect /var/run/dockershim.sock,virtlet:/run/virtlet.sock -listen /run/criproxy.sock
Restart=always
StartLimitInterval=0
RestartSec=10

[Install]
WantedBy=kubelet.service
```

You can remove `-v 3` option to reduce verbosity level of the proxy.

Then enable and start the units after stopping kubelet:
```bash
systemctl stop kubelet
systemctl daemon-reload
systemctl enable criproxy dockershim
systemctl start criproxy dockershim
```

Then we need to reconfigure kubelet. You need to pass the following extra flags to it
to make it use CRI Proxy:
```bash
--container-runtime=remote \
--container-runtime-endpoint=unix:///run/criproxy.sock \
--image-service-endpoint=unix:///run/criproxy.sock \
--enable-controller-attach-detach=false
```

In case if your cluster was deployed with kubeadm, you can typically
do this by creating a file named
`/etc/systemd/system/kubelet.service.d/20-criproxy.conf` with the
following content:

```ini
[Service]
Environment="KUBELET_EXTRA_ARGS=--container-runtime=remote --container-runtime-endpoint=/run/criproxy.sock --image-service-endpoint=/run/criproxy.sock --enable-controller-attach-detach=false"
```

Then you need to start dockershim and criproxy services, then restart kubelet:
```bash
systemctl daemon-reload
systemctl start kubelet
```

# Deploying Virtlet DaemonSet

First, you need to apply `extraRuntime=virtlet` label to each node that will run Virtlet DaemonSet (replace `XXXXXX` with the node name):
```bash
kubectl label node XXXXXX extraRuntime=virtlet
```

Then you can deploy Virtlet DaemonSet:
```bash
kubectl create -f https://raw.githubusercontent.com/Mirantis/virtlet/master/deploy/virtlet-ds.yaml
```

By default it has KVM enabled, but you can configure Virtlet to
disable it.  In order to do so, create a configmap named
`virtlet-config` in `kube-system` prior to creating Virtlet DaemonSet
that contains key-value pair `disable_kvm=y`:
```bash
kubectl create configmap -n kube-system virtlet-config --from-literal=disable_kvm=y
```

If you're using CRI Proxy bootstrap, you can watch it progress via the following command on the target node once `/var/log/criproxy-bootstrap.log` appears there:
```bash
tail -f /var/log/criproxy-bootstrap.log
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
