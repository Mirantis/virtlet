# Lifecycle of VM pod

This document describes life cycle of VM pod according to how it's handled
on level of Virtlet.

This whole description skips details of volumes preparation (done using
[flexvolumes](https://kubernetes.io/docs/concepts/storage/volumes/#flexvolume)),
access to logs/console (done by another part of Virtlet process - 
[streaming server](https://github.com/Mirantis/virtlet/tree/master/pkg/stream)),
 or port forwarding (also done by straming server).

## Assumptions

Communication between kubelet and Virtlet goes through [criproxy](https://github.com/Mirantis/criproxy)
which directs requests to Virtlet only if they match specific pod labels/annotations.

## Lifecycle

### Pod starting procedure

 * User (or some mechanism like autoscaller, deamonset controller) creates pod object.
 * Scheduler seeks for node which will fulfill all requested parameters (memory, cpu,
   matches requested node lables and so on) and allocates pod on that node.
 * `kubelet` on particular node is requested to process the pod
 * `kubelet` calls through [CRI](https://contributor.kubernetes.io/contributors/devel/container-runtime-interface/)
   the runtime service to create "sandbox" (enclosing container for all containers in pod definition),
   passing information about sandbox constraints/annotations (without actual info about containers).
 * Virtlet saves info about sandbox metadata, prepares network namespace
   and then calls through tapmanager (which operates on boundaries of both
   virtlet and pod network namespaces) [add this sandbox to your network](https://github.com/containernetworking/cni/blob/master/SPEC.md#parameters)
   command on cni plugin according to configuration in `/etc/cni/net.d` on the node
   (note: configuration from first in lexical order file in this directory, rest are ignored).
 * Plugin does it job configuring interfaces/addressation/routes/iptables/et.c.
   then retuns to runtime [info](https://github.com/containernetworking/cni/blob/master/SPEC.md#result)
   about interfaces configured and their ip configuration.
 * [`tapmanager`](https://github.com/Mirantis/virtlet/tree/master/pkg/tapmanager)
   (a goroutine inside of Virtlet process) deconstructs prepared configuration
   reconfiguring i to something more usable by VM (original configuration was
   for containers, unsuable for VMs), saves information about it for later
   calls in memory, then returns it to main part of Virtlet. It stores this
   data in own metadata store - at this point pod sandbox has ip configuration
   which can be queried by `kubelet` calls for `PodSandboxStatus`.
 * In parallel to call for sandbox creation, `kubelet` asks Virtlet (its image service)
   to download "container image" (in this case qcow2 image) as defined in container
   part of pod description.
 * Virtlet resolves image location considering configuration of [image name translation](https://github.com/Mirantis/virtlet/blob/master/docs/image-name-translation.md),
   downloads file for this location and stores it in libvirt images store.
 * When all defined in pod definition container images are downloaded
   (what is verified by `kubelet` by subsequent calls to Virtlet image service),
   `kubelet` asks Virtlet to create in particular sandbox container from downloaded image
   (note: Virtlet alows only for single "container"/vm in single pod sandbox).
 * Virtlet defines libvirt domain according to sandbox metadata (memory/cpu
   constraints) and container definition (rootfs image, volumes - preparing
   them in the same time) - without any networking and with emulator path
   set to [`vmwrapper`](https://github.com/Mirantis/virtlet/tree/master/cmd/vmwrapper),
   instead of default `qemu`.
 * When foregoing call finishes `kubelet` calls runtime to start previously created container.
 * Virtlet calls libvirt to start prepared domain. That ignites `vmwrapper` with set of parameters
   (some passed by command line, some by environment). According to these parameters vmwrapper
   asks `tapmanager` (using [gRPC](https://grpc.io/docs/guides/index.html) over unix domain socket)
   about network configuration prepared for this VM. Having this informations and using
   command line parameters, after switching it's network namespace to PodSandbox
   namespace it execs to `qemu` with new set of command line parameters.
   At this time they also include info about network devices.

At this point - VM is running and visible, pod is operating in running state, same with "container"/VM.

### Pod removal procedure

This part is ignited by user/machinery call to delete pod (`kubectl delete` pod or e.g. autoscaller force for scale down replica set):

 * Call to `apiserver` is forwarded to particular node/`kubelet` controlling particular pod
 * `kubelet` calls runtime to stop container
 * Virtlet calls libvirt to stop domain, libvirt sends signal to `qemu`,
   `qemu` is finishing it's job in some time (if it does not do that in first
   place in reasonable time - there is forcible kill called by Virtlet through
   libvirt)
 * `kubelet` checks status of "container"/VM subsequetially and when all
   containers are down - it calls runtime to `StopPodSandbox`.
 * During this call Virtlet calls (using `tapmanager`) cni plugin to remove pod
   from network.
 * `kubelet` checks status of pod sandbox subsequetially and when it notices
   that it's in stopped state, after some time (which is not constant)
   it calls Virtlet to garbage collect `PodSandbox`.
 * During that call Virtlet cleanups it's metadata about `PodSandbox`.
