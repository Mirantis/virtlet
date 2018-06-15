# Resource management

## CPU Model
By default libvirt configures qemu to use cpu model which does not allow to work nested virtualization
in VMs controlled by it. To change that you have to add annotation `VirtletCPUModel` in VM-Pod definition
with value `host-model`.

## Resource monitoring on the node
As Kubelet uses cAdvisor to collect metrics about running containers and Virtlet doesn't create container per each VM, and instead spawns VMs inside Virtlet container. This leads to all the resource usage being lumped together and ascribed to Virtlet pod.

## CPU management
### CPU cgroups facilities:
1. `shares` - relative value of cpu time assigned, not recommended for using in production as it's hard to predict the actual performance which highly depends on the neighboring cgroups.
1. `CFS CPU bandwidth control` - period and quota - hard limits.
`Parent_Period/Quota <= Child_1_Period/Quota + .. + Child_N_Period/Quota`,
where `Child_N_Period/Quota <= Parent_Period/Quota`.

### K8s CPU allocation:
1. `shares` are set per container.
1. `CFS CPU bandwidth control` - period and quota - are set per container.

**Defaults:** In absence of explicitly set values each container has 2 shares set by default.

### Libvirt CPU allocation:
1. `shares` is set per each vCPU.
1. `period` and `quota` are set per each vCPU. As libvirt imposes limits per each vCPU thread, so actual `CPU quota` is `quota` value from the domain definition times the number of vCPUs. More details re reasons of libvirt per vCPU cgroup approach can be found at https://www.redhat.com/archives/libvir-list/2015-June/msg00923.html.
1. `emulator_period` and `emulator_quota` denote the limits for emulator threads(those excluding vcpus). At the same time for unlimited domains benchmarks show that these activities may measure up to 40-80% of overall physical CPU usage by QEMU/KVM process running the guest VM.
1. vCPUs per VM - it's commonly recommended to have vCPU count set to 1 (see details in section **"CPU overcommit"** below).

**Defaults:** In absence of explicitly set values each domain has 1024 shares set by default.

#### CPU overcommit
It's outlined that linux scheduler doesn't perform well in case of CPU overcommitment and if it's not caused real need (like having multi-core VM to perform build/compile, running application inside that can effectively utilize multiple cores and was designed for parallel processing) and widely recommended to use one vCPU per VM otherwise you can expect performance degradation.

It is not recommended to have more than 10 virtual CPUs per physical processor core. Any number of overcommitted virtual CPUs above the number of physical processor cores may cause problems with certain virtualized guests, so it's always up to cluster administrators
how to set up number vCPUs per VMs.

**See more considerations on** [KVM limitations](https://docs.fedoraproject.org/en-US/Fedora/13/html/Virtualization_Guide/sect-Virtualization-Virtualization_limitations-KVM_limitations.html)

### Virtlet CPU resources management
1. By default, all VMs are created with 1 vCPU.
To change vCPU number for VM-Pod you have to add annotation `VirtletVCPUCount` with desired number, see [examples/cirros-vm.yaml](../examples/cirros-vm.yaml).
1. Due to p.2 in **"Libvirt CPU Allocation"** Virtlet spreads the assigned CPU resource limit equally among VM's vCPU threads.
1. According to p.3 in **"Libvirt CPU Allocation"** Virtlet must set limits for emulator threads(those excluding vcpus). At this time Virtlet doesn't support setting these values, but there are plans to fix this in future.

## Memory management
### K8s memory allocation
Setting memory limit to 0 or omitting it means there's no memory limit for the container.
K8s doesn't support swap on the nodes (for example, k8s creates docker containers with --memory-swappiness=0, see more at https://github.com/kubernetes/kubernetes/issues/7294).

### [Libvirt memory allocation](http://libvirt.org/formatdomain.html#elementsMemoryAllocation)
1. `memory` - allocated RAM memory at VM boot.
1. `memtune=>hard_limit` - cgroup memory limit on all domain including qemu itself usage. [However, it's claimed that such limit should be set accurately.](http://libvirt.org/formatdomain.html#elementsMemoryTuning)
1. Swap unlimited by default.

#### Memory overcommit
Overcommit memory value can reach ~150% of physical RAM amount. This relies on assumption that most processes do not access 100% of their allocated memory all the time. So you can grant guest VMs more RAM than actually is available on the host. However, this strongly depends on memory swap size available on the node and workloads of VMs memory consumptions.

For more details check [Overcommitting with KVM](https://access.redhat.com/documentation/en-US/Red_Hat_Enterprise_Linux/6/html/Virtualization_Administration_Guide/chap-Virtualization-Tips_and_tricks-Overcommitting_with_KVM.html)

### Virtlet Memory resources management
1. By default, each VM is assigned 1GB of RAM. To set other value you need set resource memory limit for container, see [examples/cirros-vm.yaml](../examples/cirros-vm.yaml).
1. Virtlet generates domain XML with memoryBacking=locked setting to prevent swapping out domain's pages.

## Summary of the action items:
1. Implement [CRI container stats methods](https://github.com/kubernetes/kubernetes/issues/27097) for Virtlet.

1. According to **2** and **3** in **"Libvirt CPU Allocation"** we need to invent some rule of setting CFS CPU bandwidth limit spread among QEMU and vCPU threads, so as to make k8s scheduler have right assumptions about the resources allocated on the node.

1. Research how to configure the [hard limits](http://libvirt.org/formatdomain.html#elementsMemoryTuning) on memory for VM pod.
