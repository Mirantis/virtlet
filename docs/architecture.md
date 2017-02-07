# Overview

Virtlet consists of the following components:

* [Virtlet manager](../cmd/virtlet) - implements CRI interface for virtualization and image handling
* [libvirt](http://libvirt.org) instance
* [VM wrapper](../cmd/vmwrapper) which is responsible for preparing environment for emulator
* emulator, currently [qemu](http://www.qemu-project.org/) with KVM support (with possibility to disable KVM)

In addition to the above, our exaple setup provide the following:

* [Images service](../contrib/deploy/image-service.yaml) which provides VM images accessible through HTTP in local cluster environment. It's only used as optional helper, because Virtlet manager can pull images from any HTTP server accessible from the node.
* [CRI proxy](../cmd/criproxy) which provides the possibility to mix docker-shim and VM based workloads on the same k8s node.

## Virtlet manager

Main binary is responsible for providing API fullfiling
[CRI specification](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/container-runtime-interface-v1.md).
It serves the requests from kubelet by doing the following:

* controls libvirt in how to prepare VM environment (virtual drives, network interfaces, trimming resources
like RAM, CPU),
* calls [CNI plugins](https://kubernetes.io/docs/admin/network-plugins/#cni) to setup network environment for virtual machines,
* tells libvirt to call VM wrapper instead of using emulator directly
* queries libvirt for VM status,
* instructs libvirt to stop VM,
* and finally calls libvirt to tear down VM environment.

## VM wrapper

Its responsibility is to:
* pass the network configuration prepared by CNI plugins to the VM by means of built-in DHCP server
* spawn emulator (currently qemu-kvm),
* respond to DHCP queries from VM,
* signal emulator to stop VM,
* reconstruct network interfaces to initial state for CNI teardown procedure.

## CRI Proxy

Provides a way to have on same kubelet controlled node CRI implementation and
docker shim, what is required to e.g. run on same node Virtlet and kube-proxy.
See [CRI Proxy design document](criproxy.md) for more detailed description.
