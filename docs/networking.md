# Networking

For now the only supported configuration is the use of
[CNI plugins](https://github.com/containernetworking/cni). To use
custom CNI configuration, mount your `/etc/cni` and optionally
`/opt/cni` into the virtlet container.

Virtlet have the same behavior and default values for `--cni-bin-dir`
and `--cni-conf-dir` as described in kubelet network plugins
[documentation](http://kubernetes.io/docs/admin/network-plugins/).

## VM Network Setup Diagram

```
+--------------------------------------------------------------------------------------------------------------------------------------------------------------+
|               +-------------------+                                                                                             Virlet                       |
|               | VM                |                                                                                             network                      |
|               |                   | Qemu process                                                                                namespace                    |
|               | +---+ eth0        |                                                                                                                          |
|               | +---+ ip addr set |                                                                                                                          |
|               |   /\              |                                                                                                                          |
|               +---||--------------+                                                                                                                          |
|                   \/                                                                                                                                         |
|               FD of the tap device                                                                                                                           |
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
|               |                                                                                                  |                                           |
|               |                +-------------------+                                                             |                                           |
|               |                |local dhcp server  |                                                             |                                           |
|               |                +-------------------+                                      pod's netns            |                                           |
|               +--------------------------------------------------------------------------------------------------+                                           |
|                                                                                                                                                              |
+--------------------------------------------------------------------------------------------------------------------------------------------------------------+
```

Virtlet uses the specified CNI plugin which is expected to create veth
pair with one end belonging to the pod network namespace.
1. On `RunPodSandBox` request Virtlet requests `tapmanager` process to
   set up the networking for the VM by sending it a command over its
   Unix domain socket
2. `tapmanager` sets up the network according to the above diagram
   (see below for more details)
3. When the VM is started, Virtlet wraps the emulator using `vmwrapper` program
   which it passes `VIRTLET_NET_KEY` environment variable containing the key
   the was used for `tapmanager` network setup.
4. `vmwrapper` uses the key to ask `tapmanager` to send it the file
   descriptor for the tap interface over `tapmanager`'s Unix domain
   socket. It then extends emulator command line arguments to make it
   use the tap device and then `exec`s the emulator.
5. Upon `StopPodSandbox`, Virtlet requests `tapmanager` to tear down
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

**NOTE:** currently Virtlet doesn't support `hostNetwork` pod setting
because it cannot be impelemnted for VM in a meaningful way.
