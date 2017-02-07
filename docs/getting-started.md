# Getting Started

## Manual setup

First way of setting up Virtlet in Kubernetes environment is based on
kubernetes local cluster, with kubelet pointed to runtime using CRI.
It has few depenencies which have to be installed manually.
To prepare such setup please follow [these](running-local-environment.md) instruction.

## Using DaemonSets

This method looks a bit more complicated, but has wider functionality
(possibility to mix container based workloads with VM based workloads on same
node, using CRI Proxy) and is better automated than previos method.
To prepare such setup please follow [these](../contrib/deploy/README.md) instruction.
