# Networking

For now the only supported configuration is the use of
[CNI plugins](https://github.com/containernetworking/cni).

Virtlet have the same behavior and default values for `--cni-bin-dir`
and `--cni-conf-dir` as described in kubelet network plugins
[documentation](http://kubernetes.io/docs/admin/network-plugins/).
These parameters refer to paths on the host, it's not necessary to
have them mounted into the Virtlet container.

## VM Network Setup Diagram

```
+--------------------------------------------------------------------------------------------------------------------------------------------------------------+
|               +-------------------+                                                                                             Virlet                       |
|               | VM                 |                                                                                            network                      |
|               |                    | Qemu process                                                                               namespace                    |
|               | +---+ eth{0,1,...} |                                                                                                                         |
|               | +---+ ip addr set  |                                                                                                                         |
|               |   /\               |                                                                                                                         |
|               +---||--------------+                                                                                                                          |
|                   \/                                                                                                                                         |
|               FDs of the tap devices                                                                                                                         |
|                                                                                                                                                              |
|                                                                                                                                                              |
|               +--------------------------------------------------------------------------------------------------+                                           |
|               |                                                                                                  |                                           |
|               |                                                            virtlet-eth0 (veth netns end          |                 veth0 (veth host end      |
|               |          +---+ tap0            +---+ br0             +---+ created by CNI)                       |           +---+ created by CNI)           |
|               |          |   |-----------------|   |-----------------|   |---------------------------------------------------|   | ip addr set               |
|               |          +---+                 +---+                 +---+                                       |           +---+                           |
|               |                              169.254.254.2/24                                                    |                                           |
|               |                                                                                                  |                                           |
|               |                                                      +---+ SR-IOV VF                             |           +---+ SR-IOV host master device |
|               |                                                      |   |---------------------------------------------------|   |                           |
|               |                                                      +---+                                       |           +---+                           |
|               |                                                                                                  |                                           |
|               |                +-------------------+                                                             |                                           |
|               |                |local dhcp server  |                                                             |                                           |
|               |                +-------------------+                                      pod's netns            |                                           |
|               +--------------------------------------------------------------------------------------------------+                                           |
|                                                                                                                                                              |
+--------------------------------------------------------------------------------------------------------------------------------------------------------------+
```

Virtlet uses the specified CNI plugin which is expected to create veth
pairs for each configured network with one end belonging to the pod network
namespace. A special case is SR-IOV plugin which puts a [Virtual Function](https://en.wikipedia.org/wiki/Single-root_input/output_virtualization)
into the pod network namespace.
1. On `RunPodSandBox` request Virtlet requests `tapmanager` process to
   set up the networking for the VM by sending it a command over its
   Unix domain socket
1. `tapmanager` sets up the network according to the above diagram
   (see below for more details)
1. `tapmanager` returns network configuration info which is used
   by Virtlet to set up Cloud-Init network config
1. When the VM is started, Virtlet wraps the emulator using `vmwrapper` program
   which it passes `VIRTLET_NET_KEY` environment variable containing the key
   the was used by `tapmanager` to set up the network.
1. `vmwrapper` uses the key to ask `tapmanager` to send it the file
   descriptors for the tap interfaces or the description of SR-IOV VFs
   over `tapmanager`'s Unix domain socket. It then extends emulator
   command line arguments to make it use tap devices/VFs and then
   `exec`s the emulator.
1. Upon `StopPodSandbox`, Virtlet requests `tapmanager` to tear down
   the VM network.

The rationale for having separate `tapmanager` process is
[the well-known Go namespace problem](https://www.weave.works/blog/linux-namespaces-and-go-don-t-mix).
It's expected that the problem will be fixed by Go 1.10 release, after
which it will be possible to dumb down `tapmanager` request processing
and run it as a goroutine (so it can use channels etc. to communicate
with Virtlet side). Currently `tapmanager` is starting automatically
by `virtlet` command using the same `virtlet` binary.

`tapmanager` performs the network setup by creating a bridge in which
it plugs CNI-provided veth (which is stripped of IP address & routes)
and a newly created tap interface. It then starts dhcp server that
listens for DHCP requests coming from tap interface, cutting the DHCP
server from the outside world with ebtables.  The dhcp server passes
CNI-provided IP, routing and DNS information to the VM so it can join
the cluster using the pod IP.

Using the scheme based on passing file descriptors over a Unix domain
socket makes it possible to have all the network related code outside
`vmwrapper` and have `vmwrapper` just `exec` the emulator instead of
spawning it as a child process.

[Calico](https://www.projectcalico.org/) CNI plugin needs special treatment
as it tries to pass a routing configuration that cannot be passed
over DHCP. For it to work Virtlet patches Calico-provided CNI result,
replacing Calico's unreachable fake gateway with another fake gateway
with an IP address acquired from Calico IPAM. A proper node subnet must
be set for Calico-based virtlet installations. It's controlled by
`calico-subnet` key Virtlet configmap (denoting the number of 1s in
the netmask) and defaults to `24`.

[SR-IOV](https://github.com/hustcat/sriov-cni) CNI plugin requires running
qemu emulator with full root privileges, so that needs to be manually enabled
during Virtlet deployment by setting `VIRTLET_SRIOV_SUPPORT` environment
variable to a non-empty value for the `virtlet` container.
In case if standard deploy/virtlet-ds.yaml is used, this can be done by settingsriov_support=true in virtlet-config ConfigMap.

**NOTE:** Virtlet doesn't support `hostNetwork` pod setting because it
cannot be implemented for VM in a meaningful way.
