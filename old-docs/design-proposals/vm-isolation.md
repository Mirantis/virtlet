# Improving VM isolation

The current `qemu.conf` used by Virtlet's libvirt deployment looks
like this:
```
stdio_handler = "file"
user = "root"
group = "root"
# we need to do network management stuff in vmwrapper
clear_emulator_capabilities = 0

cgroup_device_acl = [
         "/dev/null", "/dev/full", "/dev/zero",
         "/dev/random", "/dev/urandom",
         "/dev/ptmx", "/dev/kvm", "/dev/kqemu",
         "/dev/rtc", "/dev/hpet", "/dev/net/tun",
     ]
```

It has some apparent problems, including running VMs as root, not
dropping capabilities and extending the list of devices available to
processes in VM's cgroup. On top of that if an attacker breaks out of
a VM (s)he will be able to take the complete control of the Kubernetes
node where Virtlet pod runs, as Virtlet and Libvirt run in a single
privileged container.

There are 3 approaches to fixing it which are described in the
following sections, followed by a section with some recommendations
which are common for all the approaches.

## Using a single separate container for VMs

One possibility to reduce the attack surface is adding an extra
non-privileged container for all the VMs. This way an attacker who
escaped a VM will only be able to compromise other VMs on the same
node, but not the node itself.

When using this approach, we'll still have to use libvirt's cgroup
management mechanism, relying on libvirt API for VM resource
management. Moreover, we'll have to use libvirt's own cgroup
hierarchy.  In theory, it should be possible to tell libvirt to make
VM cgroups as children of some docker container cgroup by following
[the example](https://libvirt.org/cgroups.html#customPartiton) given in libvirt documentation,
e.g.:
```xml
<resource>
  <partition>/docker/158056e84da21859909b054fa8143f505579f6743715cc289e2231445fb4e939/vm</partition>
</resource>
```
so e.g. for `cpu` controller libvirt will use this path
`/sys/fs/cgroup/cpu/docker/158056e84da21859909b054fa8143f505579f6743715cc289e2231445fb4e939/vm.partition`

Unfortunately this doesn't work because libvirt adds `.partition`
suffix not just to the last item in the path but to every part or it,
so it tries to use
`/sys/fs/cgroup/cpu/docker.partition/158056e84da21859909b054fa8143f505579f6743715cc289e2231445fb4e939.partition/vm.partition`
and apparently this behavior can't be overridden. Thus, we'll have to
use default libvirt cgroup hierarchy, and any resource limits imposed
on the VM container itself will not affect the VMs.

The plan of this approach is to make a separate container for VMs in
Virtlet DaemonSet definitions and then make `vmwrapper` perform extra
steps instead of just executing the actual emulator:

1. Find a process running inside the VM container
2. Find the required emulator binary and open it for reading
3. Use
   [nsenter-like](https://github.com/docker/libcontainer/tree/6aeb7e1fa51f04f1253f79fc86da4b608fcb3b59/nsenter)
   mechanism to enter the namespaces of the process that runs in VM
   container, including mount namespace
4. Using `devices` cgroup controller to disable access to the devices
   that are not needed for VM (although probably needed for
   `vmwrapper`'s network setup mechanism)
5. Drop capabilities of the current process (another option: switch to
   a non-root user).
6. Execute emulator binary using `/proc/self/fd/NNN`, where `NNN` is
   the file descriptor from step 2 (that's what
   [fexecve()](https://github.com/lattera/glibc/blob/a2f34833b1042d5d8eeb263b4cf4caaea138c4ad/sysdeps/unix/sysv/linux/fexecve.c#L28)
   does on Linux). This way, we may avoid the need for emulator binary
   to be available in the VM container.

A non-virtlet simple image like 'busybox' should be used for VM
container (the emulator itself). The VM container must be able to
access paths used by the emulator, such as volumes and monitor socket.

Pros:
* Relatively easy to implement
* Provides reasonable level of security

Cons:
* Escaping single VM means controlling all the VMs on the node
* Need to implement libvirt-specific resource monitoring functionality
  (`ContainerStats()`, `ListContainerStats()` and `ImageFsInfo()` CRI
  calls)

## Using dedicated QEMU/KVM container per VM with libvirt cgroups

This approach basically repeats the previous one, with one important
difference: instead of just starting a single VM container as part of
a Virtlet pod, we use Docker API to run a new container for each VM.
The mechanics of vmwrapper remains the same, differing only in how PID
of a process inside target container is obtained.

Pros:
* Provides better level of security than single container for all VMs
  as escaping the VM will only lead to compromise of a single container

Cons:
* Harder to implement than single container for all VMs
* Need to access docker socket
* Need to implement libvirt-specific resource monitoring functionality
  (`ContainerStats()`, `ListContainerStats()` and `ImageFsInfo()` CRI
  calls)
  
For more info on implementing resource monitoring, see
[the corresponding section](http://libvirt.org/apps.html#monitoring)
in libvirt documentation. But basically we can just tap directly into
libvirt cgroup hierarchy to get CPU and memory usage, and for
filesystem usage just look at the number and size of the volumes in
use.

## Using dedicated QEMU/KVM container per VM with docker cgroups

This approach is the same as the previous one except that we also use
Docker cgroups for VM containers. In this case, libvirt cgroups just
don't do anything and we use the mechanisms closely resembling those
used in standard kubelet dockershim for resource limits and
monitoring.

Pros:
* Provides better level of security than single container for all VMs
  as escaping the VM will only lead to compromise of a single container
* Resources are managed in standard Kubernetes way, we just mimic
  kubelet's dockershim

Cons:
* Harder to implement than single container for all VMs
* Need to access docker socket
* Need to redo resource limits in Virtlet

## Additional security measures

The following additional security measures can be taken no matter what
approach we take:
* Use separate container for libvirt. This entails changing how we
  prepare the tap fd in vmwrapper because mounting network namespace
  directory can be problematic in some cases (e.g. because of one of
  `/run` or `/var/run` being a symbolic link). Basically tap fd must
  be prepared on Virtlet side and then sent over a Unix domain socket
  to vmwrapper process (the socket may reside on an `emptyDir`
  volume). With current version of Go this is somewhat complicated
  because the problem with switching namespaces inside Go process, so
  this will mean starting a subprocess that will prepare and sned the
  file descriptor.
* Use Unix domain socket for libvirt. Currently we use TCP socket and
  listen for connections on `localhost`, thus providing additional
  possibilities for an attack
* Add AppArmor/SELinux support out of the box

## Recommendations

The recommendation is to begin with the first approach as the easiest
one, moving to container-per-VM approach with libvirt cgroups later
when we can. After we have container per VM, we need to decide on
whether moving to docker cgroups will really help us.

*Additional security measures* need to be implemented, too. The
changes may be done in any sequence.

## Appendix

CRI data structures relevant to resource usage statistics:

```protobuf
// ContainerAttributes provides basic information of the container.
message ContainerAttributes {
    // ID of the container.
    string id = 1;
    // Metadata of the container.
    ContainerMetadata metadata = 2;
    // Key-value pairs that may be used to scope and select individual resources.
    map<string,string> labels = 3;
    // Unstructured key-value map holding arbitrary metadata.
    // Annotations MUST NOT be altered by the runtime; the value of this field
    // MUST be identical to that of the corresponding ContainerConfig used to
    // instantiate the Container this status represents.
    map<string,string> annotations = 4;
}

// ContainerStats provides the resource usage statistics for a container.
message ContainerStats {
    // Information of the container.
    ContainerAttributes attributes = 1;
    // CPU usage gathered from the container.
    CpuUsage cpu = 2;
    // Memory usage gathered from the container.
    MemoryUsage memory = 3;
    // Usage of the writeable layer.
    FilesystemUsage writable_layer = 4;
}

// CpuUsage provides the CPU usage information.
message CpuUsage {
    // Timestamp in nanoseconds at which the information were collected. Must be > 0.
    int64 timestamp = 1;
    // Cumulative CPU usage (sum across all cores) since object creation.
    UInt64Value usage_core_nano_seconds = 2;
}

// MemoryUsage provides the memory usage information.
message MemoryUsage {
    // Timestamp in nanoseconds at which the information were collected. Must be > 0.
    int64 timestamp = 1;
    // The amount of working set memory in bytes.
    UInt64Value working_set_bytes = 2;
}

// FilesystemUsage provides the filesystem usage information.
message FilesystemUsage {
    // Timestamp in nanoseconds at which the information were collected. Must be > 0.
    int64 timestamp = 1;
    // The underlying storage of the filesystem.
    StorageIdentifier storage_id = 2;
    // UsedBytes represents the bytes used for images on the filesystem.
    // This may differ from the total bytes used on the filesystem and may not
    // equal CapacityBytes - AvailableBytes.
    UInt64Value used_bytes = 3;
    // InodesUsed represents the inodes used by the images.
    // This may not equal InodesCapacity - InodesAvailable because the underlying
    // filesystem may also be used for purposes other than storing images.
    UInt64Value inodes_used = 4;
}
```

## References

The initial research of Virtlet security was done by [Adam Heczko](mailto:aheczko@mirantis.com).
