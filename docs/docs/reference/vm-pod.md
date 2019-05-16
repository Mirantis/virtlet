# Differences between "plain" Kubernetes pods and VM pods

Virtlet tries hard to make VM pods appear as plain Kubernetes
pods. Still, there are some important differences, including:

1. VM pods can have just one "container"
1. You can't use container images for VM pods
1. Some container-specific settings such as network/PID/IPC
   namespaces, SELinux/AppArmor settings, privileged flag etc. aren't
   applicable to VM pods
1. Some volume types are handled differently. There are
   VM-pod-specific settings such as Cloud-Init, persistent rootfs, etc.
   For more information, see [Volumes](../volumes/).
1. `kubectl exec` and exec readiness/liveness probes aren't supported
   for VM pods yet
1. There are VM-pod-specific settings such as
   [Cloud-Init](../cloud-init/), persistent rootfs, etc.

Another important point is that when using a persistent root
filesystem, the lifetime of the VM is not limited to that of the pod.

Despite these differences, there are quite a few Kubernetes features
that work for VM pods just as well as for "plain" pods, for example,
most `kubectl` commands, pointing services at VM pods, and so on.

Besides `kubectl`, a Virtlet-specific tool called
[virtletctl](../virtletctl/) can be used to perform Virtlet-specific
actions on VM pods and Virtlet processes in the cluster, such as
connecting to the VM using SSH and providing VNC connection and
dumping cluster diagnostic info.

# Supported kubectl commands

Most `kubectl` commands' behavior doesn't differ between "plain" and
VM pods. Exceptions are `kubectl exec` which isn't supported at the
moment, and `kubectl attach` / `kubectl logs` which work only if the
VM has serial console configured.

`kubectl attach` attaches to the VM serial console. Detaching from the
console is done via `Ctrl-]`.

`kubectl logs` displays the logs for the pod. In case of VM pod, the
log is the serial console output. `kubectl logs -f`, which follows the
log as it grows, is supported, too.

# Using higher-level Kubernetes objects

One of the advantages of pod-based approach to running VMs on
Kubernetes is an ability to use higher-level Kubernetes objects such
as StatefulSets, Deployments, DaemonSets etc. trivially with VMs.

Virtlet includes a
[nested Kubernetes example](https://github.com/Mirantis/virtlet/blob/master/examples/k8s.yaml)
which makes a nested Kubernetes cluster using a
[StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
of 3 VM pods, which are initialized using
[kubeadm](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/).

In the k8s-in-k8s example, first we create a
[headless service](https://kubernetes.io/docs/concepts/services-networking/service/#headless-services)
that will point domain names `k8s-0`, `k8s-1` and `k8s-2` as resolved
by cluster's DNS to the corresponding StatefulSet replicas:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: k8s
  labels:
    app: k8s
spec:
  ports:
  - port: 22
    name: ssh
  clusterIP: None
  selector:
    app: inner-k8s
```

Then, we begin defining a StatefulSet with 3 replicas:
```
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: k8s
spec:
  serviceName: k8s
  replicas: 3
  selector:
    matchLabels:
      app: inner-k8s
```

The pods that comprise the StatefulSet are VM pods and thus
must have `kubernetes.io/target-runtime: virtlet.cloud` annotation.
Also, we set the
root volume size to 4Gi to have some place for Docker images:
```yaml
  template:
    metadata:
      labels:
        app: inner-k8s
      annotations:
        kubernetes.io/target-runtime: virtlet.cloud
        # set root volume size
        VirtletRootVolumeSize: 4Gi
```

Then we add another annotation that will contain
[Cloud-Init](../cloud-init/) user-data, in which we write some files
to adjust Docker settings and add the Kubernetes repository for apt,
as well as a provisioning script that will install the necessary
packages and then run `kubeadm init` on `k8s-0` and `kubeadm join` on
`k8s-1` and `k8s-2`. The script makes use of StatefulSet's
[stable network IDs](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#stable-network-id),
so the nodes have names `k8s-0`, `k8s-1` and `k8s-2` that can be
resolved by cluster DNS.

```yaml
        VirtletCloudInitUserData: |
          write_files:
          - path: /etc/systemd/system/docker.service.d/env.conf
            permissions: "0644"
            owner: root
            content: |
              [Service]
              Environment="DOCKER_OPTS=--storage-driver=overlay2"
          - path: /etc/apt/sources.list.d/kubernetes.list
            permissions: "0644"
            owner: root
            content: |
              deb http://apt.kubernetes.io/ kubernetes-xenial main
          - path: /usr/local/bin/provision.sh
            permissions: "0755"
            owner: root
            content: |
              #!/bin/bash
              set -u -e
              set -o pipefail
              curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add -
              apt-get update
              apt-get install -y docker.io kubelet kubeadm kubectl kubernetes-cni
              sed -i 's/--cluster-dns=10\.96\.0\.10/--cluster-dns=10.97.0.10/' \
                  /etc/systemd/system/kubelet.service.d/10-kubeadm.conf
              systemctl daemon-reload
              if [[ $(hostname) =~ -0$ ]]; then
                # master node
                kubeadm init --token adcb82.4eae29627dc4c5a6 \
                             --pod-network-cidr=10.200.0.0/16 \
                             --service-cidr=10.97.0.0/16 \
                             --apiserver-cert-extra-sans=127.0.0.1,localhost
                export KUBECONFIG=/etc/kubernetes/admin.conf
                export kubever=$(kubectl version | base64 | tr -d '\n')
                kubectl apply -f "https://cloud.weave.works/k8s/net?k8s-version=$kubever"
                while ! kubectl get pods -n kube-system -l k8s-app=kube-dns |
                    grep ' 1/1'; do
                  sleep 1
                done
                mkdir -p /root/.kube
                chmod 700 /root/.kube
                cp "${KUBECONFIG}" /root/.kube/config
                echo "Master setup complete." >&2
              else
                # worker node
                kubeadm join --token adcb82.4eae29627dc4c5a6 \
                             --discovery-token-unsafe-skip-ca-verification k8s-0.k8s:6443
                echo "Node setup complete." >&2
              fi
```

We then add an ssh public key (corresponding to `examples/vmkey`) for
the user `root`:

```
          users:
          - name: root
            # VirtletSSHKeys only affects 'ubuntu' user for this image, but we want root access
            ssh-authorized-keys:
            - ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCaJEcFDXEK2ZbX0ZLS1EIYFZRbDAcRfuVjpstSc0De8+sV1aiu+dePxdkuDRwqFtCyk6dEZkssjOkBXtri00MECLkir6FcH3kKOJtbJ6vy3uaJc9w1ERo+wyl6SkAh/+JTJkp7QRXj8oylW5E20LsbnA/dIwWzAF51PPwF7A7FtNg9DnwPqMkxFo1Th/buOMKbP5ZA1mmNNtmzbMpMfJATvVyiv3ccsSJKOiyQr6UG+j7sc/7jMVz5Xk34Vd0l8GwcB0334MchHckmqDB142h/NCWTr8oLakDNvkfC1YneAfAO41hDkUbxPtVBG5M/o7P4fxoqiHEX+ZLfRxDtHB53 me@localhost
```

and make [Cloud-Init](../cloud-init/) run the provisioning script when
the VM boots:

```
          runcmd:
          - /usr/local/bin/provision.sh
```

After that, we get to the pod spec, in which we limit the
possibilities of running the pod to the nodes with
`extraRuntime=virtlet` label where Virtlet daemon pods are placed and
use Ubuntu 16.04 image:

```yaml
    spec:
      nodeSelector:
        extraRuntime: virtlet
      containers:
      - name: ubuntu-vm
        image: virtlet.cloud/cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img
        imagePullPolicy: IfNotPresent
        # tty and stdin required for `kubectl attach -t` to work
        tty: true
        stdin: true
```

We then specify a readiness probe which will mark each pod as `Ready`
once its ssh port becomes active:

```yaml
        readinessProbe:
          tcpSocket:
            port: 22
          initialDelaySeconds: 5
```

We could use a more sophisticated check, e.g. by making sure that
apiserver is accessible, but for the purpose of the example we use
this trivial scheme to keep things simple. Note that `kubeadm join` on
`k8s-1` and `k8s-2` will keep retrying till `kubeadm init` on `k8s-0`
completes its task.

In order to test the example, we start it and wait till `k8s-0`,
`k8s-1` and `k8s-2` pods appear:

```console
$ kubectl apply -f examples/k8s.yaml
$ kubectl get pods -w
```

Then we can view the logs on each of the node to see the progress
of Kubernetes setup, e.g.
```console
$ kubectl logs -f k8s-0
...
[  226.115652] cloud-init[1513]: Master setup complete.
...
```

After the setup is complete, we can use [virtletctl](../virtletctl/)
to ssh into the master VM and check the cluster:
```console
$ virtletctl ssh root@k8s-0 -- -i examples/vmkey
Welcome to Ubuntu 16.04.5 LTS (GNU/Linux 4.4.0-138-generic x86_64)

...

root@k8s-0:~# kubectl get pods --all-namespaces
NAMESPACE     NAME                            READY   STATUS    RESTARTS   AGE
kube-system   coredns-576cbf47c7-8dnh6        1/1     Running   0          69m
kube-system   coredns-576cbf47c7-q622f        1/1     Running   0          69m
kube-system   etcd-k8s-0                      1/1     Running   0          69m
kube-system   kube-apiserver-k8s-0            1/1     Running   0          68m
kube-system   kube-controller-manager-k8s-0   1/1     Running   0          68m
kube-system   kube-proxy-4dgfx                1/1     Running   0          69m
kube-system   kube-proxy-jmw6c                1/1     Running   0          69m
kube-system   kube-proxy-qwbw7                1/1     Running   0          69m
kube-system   kube-scheduler-k8s-0            1/1     Running   0          68m
kube-system   weave-net-88jv4                 2/2     Running   1          69m
kube-system   weave-net-kz698                 2/2     Running   0          69m
kube-system   weave-net-rbnmf                 2/2     Running   1          69m
root@k8s-0:~# kubectl get nodes
NAME          STATUS   ROLES    AGE   VERSION
kube-master   Ready    master   29m   v1.14.1
kube-node-1   Ready    <none>   28m   v1.14.1
kube-node-2   Ready    <none>   28m   v1.14.1
```
