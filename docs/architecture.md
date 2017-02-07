# Overview

Whole Virtlet solution consists of following elements:

* [Virtlet manager](../cmd/virtlet) - implementing CRI interface for virtualisation and images handling
* [libvirt](http://libvirt.org) instance
* [VM wrapper](../cmd/vmwrapper) whos responsibility is to prepare environment for VM runner
* VM runner, currently [qemu](http://www.qemu-project.org/) with KVM support (with possibility to disable KVM)

Our example setup additionally provide:

* [Images service](../contrib/deploy/image-service.yaml) which provides VM images accessible through HTTP in local cluster environment. It's only used as faciliation, because Virtlet manager can pull images through HTTP from any accessible for node place.
* [CRI proxy](../cmd/criproxy) which provides possibility to mix docker shim based workloads with VM based on same kubelet instance.

## Virtlet manager

Main binary is responsible for providing API fullfiling
[CRI specification](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/container-runtime-interface-v1.md).
Basing on requests from kubelet, it:

* controls libvirt in how to prepare VM environment (virtual drives, network interfaces, trimming resources
like RAM, CPU),
* calls [CNI plugins](https://kubernetes.io/docs/admin/network-plugins/#cni) to base setup networking setup,
* controls libvirt to spawn VM runner using VM wrapper,
* queries libvirt for VM status,
* instructs libvirt to stop VM,
* and finally calls libvirt to tear down VM environment.

## VM wrapper

Its responsibility is to:
* reconstruct network interfaces configured by CNI plugins from container compatible to VM compatible (in network namespace),
* spawn VM runner (currently qemu-kvm),
* respond to DHCP queries from VM,
* signal VM runner to stop VM,
* reconstruct network interfaces to initial state for CNI tear down procedure.

## CRI Proxy

Provides a way to have on same kubelet controlled node CRI implementation and
docker shim, what is required to e.g. run on same node Virtlet and kube-proxy.
Deeper description in [this](criproxy.md) document.
