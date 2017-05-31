 # Resource management
 ## Resource monitoring on node
 All data about current resource usage are collected by cadvisor, so when VM is started in Virtlet container (it's not using a separate container for VM) - cadvisor will return only resources used by `vmwrapper`, which uses this container only during pod start/cleanup
  (networking setup/teardown). This means that scheduler has info that virtlet controlled pods do not use any RAM/cpu when there are no limits set in pod definition.

 ## CPU management
 ### CPU cgroups facilities:
 1. `shares` - relative value of cpu time assigned, not recommended for using in production as it's hard to predict actual performance as it highly depends on neighbours groups.
 1. `CFS CPU bandwidth control` - period and quota - hard limits.
 `Parent_Period/Quota >= Child_1_Period/Quota + ..+ Child_N_Period/Quota`,
 where `Child_N_Period/Quota <= Parent_Period/Quota`.

 ### K8s CPU allocation:
 1. `shares` set per container.
 1. `CFS CPU bandwidth control` - period and quota - set per container.

 **Defaults:** In absence of explicitly set values each container has 2 shares set by default.

 ### Libvirt CPU allocation:
 1. `shares` is set per each vCPU.
 1. `period` and `quota` are set per each vCPU =>  comparing to containers resources consumed by VM with current settings will be greater in numvCPUs times per VM then per container. (More details re reasons of libvirt per-vCPU cgroup approach https://www.redhat.com/a
 rchives/libvir-list/2015-June/msg00923.html).
 1. `emulator_period` and `emulator_quota` - for emulator threads => for each VM we have extra consuming by qemu threads and the amount is currently unlimited. At the same time benchmarking can show that on unlimited domains, qemu threads depending on type of operatio
 n (disk or networking I/O) can consume from ~40% to ~80% from overall guest VM physical CPU usage.
 1. vCPUs per VM - commonly recommended to have 1 set (see details in section **"CPU overcommit"** below).

 **Defaults:** In absence of explicitly set values each domain has 1024 shares set by default.

 #### CPU overcommit
 It's outlined that linux scheduler doesn't perform well in case of CPU overcommitment and if it's not caused real need (like having multi-core VM to perform build/compile, running application inside that can effectively utilize multiple cores and was designed for par
 allel processing) and widely recommended to use one vCPU per VM otherwise you can expect performance degradation.

 It is not recommended to have more than 10 virtual CPUs per physical processor core. Any number of overcommitted virtual CPUs above the number of physical processor cores may cause problems with certain virtualized guests, so it's always up to cluster administrators
 how to set up number vCPUs per VMs.

 **See more considerations on** [KVM limitations](https://docs.fedoraproject.org/en-US/Fedora/13/html/Virtualization_Guide/sect-Virtualization-Virtualization_limitations-KVM_limitations.html)

 ### Virtlet CPU resources management
 1. By default, all VMs are created with 1 vCPU.
 To change vCPU number for VM-Pod you have to add annotation `VirtletVCPUCount `with desired number, see [examples/cirros-vm.yaml](../examples/cirros-vm.yaml).
 1. Taking into account **2** in **"Libvirt CPU Allocation"** Virtlet divides equally assinged cpu resource limit among VM's vCPU threads.

 ## Memory management
 ### K8s memory allocation
 Omitting memory setting, i.e. passed value 0, leads to unlimited memory usage for container
 doesn't support container memory swap, when using docker container is created with --memory-swappiness=0 (see more https://github.com/kubernetes/kubernetes/issues/7294).

 ### [Libvirt memory allocation](http://libvirt.org/formatdomain.html#elementsMemoryAllocation)
 1. `memory` - allocated RAM memory at VM boot.
 1. `memtune=>hard_limit` - cgroup memory limit on all domain including qemu itself usage. However, it's claimed that such limit should be set accurately.
 1. Swap unlimited by default.

 #### Memory overcommit
 Overcommit memory to a ~150% of physical RAM. But with the assumption that not all VMs are using all their allocated memory at the same time. However, this strongly depends on memory swap size available on the node and workloads of VMs memory consumptions.

 For more details check [Overcommitting with KVM](https://access.redhat.com/documentation/en-US/Red_Hat_Enterprise_Linux/6/html/Virtualization_Administration_Guide/chap-Virtualization-Tips_and_tricks-Overcommitting_with_KVM.html)

 ### Virtlet Memory resources management
 1. By default, each VM is assugned to 1GB of RAM. To set other value you need set resource memory limit for container, see [examples/cirros-vm.yaml](../examples/cirros-vm.yaml).
 2. Virtlet generate domain's xml with setting "memoryBacking"=>locked to prevent swapping out domain's pages.

 ## Summary of planned action items:
 1. Implement [CRI container stats methods](https://github.com/kubernetes/kubernetes/issues/27097) for Virtlet.

 1. According to **2** and **3** in **"Libvirt CPU Allocation"** we need to elaborate some rule of setting CFS CPU bandwidth limits spreaded among qemu and vcpu threads, thus k8s scheduler could have right assumptions on allocated resources on the node.

 1. Research on how to set up memory [hard limits](http://libvirt.org/formatdomain.html#elementsMemoryTuning) for VM-pod.
