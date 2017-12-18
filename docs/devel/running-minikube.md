# Running local environment for Virtlet

## Pre-run steps

At this stage Virtlet have following requirements:

* SELinux/AppArmor disabled on host (to disable them - follow documentation from your host Linux distribution),
* if host have libvirt installed - it should be stopped when working with Virtlet,
* [docker](https://www.docker.com) should be installed on host and user account on which Virtlet will be built and run - should be properly configured to use this docker installation (possibly adding user's account into group in which docker deamon is running should be enough, but please follow docker documentation for your host Linux distribution),
* [CRI-Proxy](https://github.com/Mirantis/criproxy) needs to be installed in the host.
* [minikube](https://github.com/kubernetes/minikube) has to be installed in the host machine.

In order to disable AppArmor you can execute the following lines:
```
service apparmor stop
service apparmor teardown
update-rc.d -f apparmor remove
```
## Startup minikube
You can get the cluster startup script like this:
```
curl -Lo minikube https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64 && chmod +x minikube
minikube start --vm-driver=none --extra-config=kubelet.ContainerRuntime=remote --extra-config=kubelet.RemoteRuntimeEndpoint=/run/criproxy.sock
```

## Running virlet
In order to start locally-built Virtlet and CRI proxy on `minikube`:
```
kubectl label node ubuntu extraRuntime=virtlet
curl https://raw.githubusercontent.com/Mirantis/virtlet/master/deploy/images.yaml >images.yaml
kubectl create configmap -n kube-system virtlet-image-translations --from-file images.yaml
kubectl apply -f virtlet-ds.yaml
kubectl create configmap -n kube-system virtlet-config --from-literal=disable_kvm=y
```
