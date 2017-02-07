# Getting Started

## Manual setup

One way of setting up Virtlet in Kubernetes environment is based on
kubernetes local cluster, with kubelet pointed to runtime using CRI.
It has few depenencies which have to be installed manually.
To prepare this setup please follow [these](running-local-environment.md) instructions.

## Using DaemonSets

This method looks a bit more complicated, but has wider functionality
(possibility to mix container based workloads with VM based workloads on same
node, using CRI Proxy) and is better automated than first method.
To prepare this setup please follow [these](../contrib/deploy/README.md) instructions.
