## Introduction

Virtlet is a Kubernetes runtime server which allows you to run VM
workloads on your cluster. It can run arbitrary QCOW2 images, while
making the VMs appear as pod-like as possible. This means the
possibility of using most of standard `kubectl` commands, building
higher-level Kubernetes objects such as StatefulSets or Deployments
out of the VMs and so on. On the other hand, it's also suitable for
running the traditional long-lived "pet" VMs, too.

Virtlet has full support for Kubernetes networking and multiple CNI
implementations, such as Calico, Weave and Flannel. In addition to this,
more advanced CNI setups are supported, too, such as SR-IOV and using
multiple CNI implementations at the same time.

You can a Virtlet usage demo by following
[this link](https://asciinema.org/a/1a6xp5j4o22rnsx9wpvumd4kt).
