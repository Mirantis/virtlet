# Lifecycle of a VM pod

This document describes the lifecycle of VM pod managed by Virtlet.

This description omits the details of volume setup (using
[flexvolumes](https://kubernetes.io/docs/concepts/storage/volumes/#flexvolume)),
handling of logs, the VM console and port forwarding (done by
[streaming server](https://github.com/Mirantis/virtlet/tree/master/pkg/stream)),
 or port forwarding.

## Assumptions

Communication between kubelet and Virtlet goes through [criproxy](https://github.com/Mirantis/criproxy)
which directs requests to Virtlet only if the requests concern a pod that has
Virtlet-specific annotation or an image that has Virtlet-specific prefix.

## Lifecycle

### VM Pod Startup

 * A pod is created in Kubernetes cluster, either directly by the user or via
   some other mechanism such as a higher-level Kubernetes object managed by
   `kube-controller-manager` (ReplicaSet, DaemonSet etc.).
 * Scheduler places the pod on a node based on the requested resources
   (CPU, memory, etc.) as well as pod's nodeSelector and pod/node affinity
   constraints, taints/tolerations and so on.
 * `kubelet` running on the target node accepts the pod.
 * `kubelet` invokes a [CRI](https://contributor.kubernetes.io/contributors/devel/container-runtime-interface/)
   call RunPodSandbox to create the pod sandbox which
   will enclose all the containers in the pod definition. Note that at this
   point no information about the containers within the pod is passed
   to the call. `kubelet` can later request the information about the pod
   by means of `PodSandboxStatus` calls.
 * If there's a Virtlet-specific annotation `kubernetes.io/target-runtime: virtlet.cloud`,
   CRI proxy passes the call to Virtlet.
 * Virtlet saves sandbox metadata in its internal database, sets up the
   network namespace and then uses internal `tapmanager` mechanism to invoke
   `ADD` operation via the CNI plugin as specified by the
   CNI configuration on the node.
 * The CNI plugin configures the network namespace by setting up
   network interfaces, IP addresses, routes, iptables rules and so on,
   and returns the network configuration information to the caller as described
   in the [CNI spec](https://github.com/containernetworking/cni/blob/master/SPEC.md#result).
 * Virtlet's [`tapmanager`](https://github.com/Mirantis/virtlet/tree/master/pkg/tapmanager)
   mechanism adjusts the configuration of the network namespace to make it work with the VM.
 * After creating the sandbox, kubelet starts the containers defined in
   the pod sandbox. Currently, Virtlet supports just one container per VM pod.
   So, the VM pod startup steps after this one describe the startup of this single container.
 * Depending on the image pull police of the container, kubelet checks if
   the image needs to be pulled by means of `ImageStatus` call and then uses
   `PullImage` CRI call to pull the image if it doesn't exist or if
   `imagePullPolicy: Always` is used.
 * If `PullImage` is invoked, Virtlet resolves the image location based on the
   [image name translation configuration](https://github.com/Mirantis/virtlet/blob/master/docs/image-name-translation.md),
   then downloads the file and stores it in the image store.
 * After the image is ready (no pull was needed or the `PullImage` call completed
   successfully), kubelet uses `CreateContainer` CRI call to create
   the container in the pod sandbox using the specified image.
 * Virtlet uses the sandbox and container metadata to generate libvirt domain definition,
   using [`vmwrapper`](https://github.com/Mirantis/virtlet/tree/master/cmd/vmwrapper)
   binary as the emulator and without specifying any network configuration in the domain.
 * After `CreateContainer` call completes, `kubelet` invokes `StartContainer` call
   on the newly created container.
 * Virtlet starts the libvirt domain. libvirt invokes `vmwrapper` as the emulator,
   passing it the necessary command line arguments as well as environment variables
   set by Virtlet. `vmwrapper` uses the environment variable values passed
   to Virtlet to communicate with `tapmanager` over an Unix domain socket,
   retrieving a file descriptor for a tap device and/or pci address of SR-IOV
   device set up by `tapmanager`. `tapmanager` uses its own simple protocol to
   communicate with `vmwrapper` because it needs to send file descriptors over
   the socket. This is not usually supported by RPC libraries, see e.g.
   [grpc/grpc#11417](https://github.com/grpc/grpc/issues/11417).
   `vmwrapper` then updates the command line arguments to include the network
   interface information and execs the actual emulator (`qemu`).

At this point the VM is running and accessible via the network, and the pod is
in `Running` state as well as it's only container.

### Deleting the pod

This sequence is initiated when the pod is deleted, either by means of `kubectl delete`
or a controller manager action due to deletion or downscaling of a higher-level object.

 * `kubelet` notices the pod being deleted.
 * `kubelet` invokes `StopContainer` CRI calls which is getting forwared
   to Virtlet based on the containing pod sandbox annotations.
 * Virtlet stops the libvirt domain. libvirt sends a signal to `qemu`, which initiates
   the shutdown. If it doesn't quit in a reasonable time determined by pod's
   termination grace period, Virtlet will forcibly terminate the domain,
   thus killing the `qemu` process.
 * After all the containers in the pod (the single container in case of
   Virtlet VM pod) are stopped, kubelet invokes `StopPodSandbox` CRI call.
 * Virtlet asks its `tapmanager` to remove pod from the network by means of
   `CNI DEL` command.
 * after `StopPodSandbox` returns, the pod sandbox will be eventually GC'd
   by `kubelet` by means of `RemovePodSandbox` CRI call.
 * Upon `RemovePodSandbox`, Virtlet removes the pod metadata from its internal database.
