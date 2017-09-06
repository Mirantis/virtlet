# Exposing VM metrics as CRI metrics

Kubernetes historically uses `cAdvisor` as a main tool to gather container performance metrics.
With introduction of CRI, new container runtimes can be plugged, including those not supported
by `cAdvisor`. Thus there become a need to extend CRI with ability for runtimes to report their
own core performance metrics. The interface was added in Kubernetes 1.7 and it's expected to be
consumed and integrated into Kubernetes monitoring pipeline by 1.8 release.

Since `virtlet` is implemented as container runtime and virtlet VMs pretend to be normal pods,
it makes sense to implement CRI metrics interface so that virtlet VMs could be monitored with
the same tools that are used for docker containers. Since the interface is already released,
there is nothing that blocks from implementing it right away.

## CRI metrics interface

Main CRI interface was extended with two additional methods: `ContainerStats` that returns
metrics for particular container and `ListContainerStats`, which returns a list of container
metrics for all containers hosted by the target runtime and matching the given filter, which
may include sandbox id and a label map.

In all cases, the result is either one or collection of `ContainerStats` structures that is
comprised of CPU, memory and disk usage metrics, each with its own timestamp.

## Metrics gathering rate

Virtlet should gather the required metrics asynchronously rather than upon request from, `kubelet`.
The frequency, at which metrics are going to be fetched by `kubelet` is yet unknown. Thus it makes
sense to have frequency at which virtlet collects the metrics be configurable. If this frequency
is set to a higher value than that of `kubelet` requests, we will end up collecting several
samples between requests. In this case, the intermediate samples should be aggregated.
For CPU usage it makes sense to return average usage from collected samples (along with mean timestamp),
whereas for disk and memory the last sample is more representative. After each `kubelet` requests,
all collected samples are purged.

However, since the CRI metrics are optional and do not implemented in 1.7, virtlet must be ready that
there won't be any `kubelet` queries at all. Thus we should keep in memory only `N` last collected
samples. To make algorithm simpler and more flexible at the same time, we can make `N` be configurable
for each metric type separately and always return average value over all collected samples, but have
default `N` values be 1 for disk and memory and about 3 for CPU.

## Obtaining metrics values

Virtlet can use `libvirt` API to get required core metrics from VMs.
For each domain, cumulative CPU usage can be taken from `Cpu.Time` field. As another choice, CPU
information can be also retieved with `GetCPUStats` libvirt function.

Memory statistics are available through `GetMemoryStats` function.

As for the disks, the current interface makes use of `FilesystemUsage` structure, which was used in the
`ImageService` interface to return storage, occupied by the images in both bytes and inodes as well as
ID of the underlying storage. It is not clear, how kubernetes is going to use this structure for disk
metrics. Meanwhile we could set only the most important field `used_bytes` and ignore the rest.
We can use `virt-df` from `libguestfs` to retrieve free space information from VM.
