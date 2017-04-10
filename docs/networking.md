# Networking

For now the only supported configuration is the use of [CNI plugins](https://github.com/containernetworking/cni). To use custom CNI configuration, mount your `/etc/cni` and optionally `/opt/cni` into the virtlet container.
Virtlet have the same behavior and default values for `--cni-bin-dir` and `--cni-conf-dir` as described in kubelet network plugins [documentation](http://kubernetes.io/docs/admin/network-plugins/).

## Netwoking scheme

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
|               FD of opened tap deivce                                                                                                                        |
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

Current workflow using CNI plugin which is expected to create veth pair with one end belongs to pod network namespace:

 - On RunPodSandBox request virtlet creates ne netns with name equal to PodId and calls CNI plugin to create veth pair and allocate ips
 - On StartContainer request virtlet prepares domain xml definition with emulator set to VMWrapper (separate binary to prepare network for VM using CNI veth pair and ips) and several env vars set thru qemu commandline
```
....
    <devices>
        <emulator>/vmwrapper</emulator>
...
<commandline xmlns='http://libvirt.org/schemas/domain/qemu/1.0'>
      <env name='VIRTLET_EMULATOR' value='%s'/>
      <env name='VIRTLET_NS' value='%s'/>
      <env name='VIRTLET_CNI_CONFIG' value='%s'/>
</commandline>
...

where
- VIRTLET_EMULATOR - the fully qualified path to the device model emulator binary, ex: /usr/bin/kvm.
- VIRTLET_NS - the fully qualified path to the netns, ex: /var/run/netns/8d6f7a19-c865-11e6-ae2c-02424d6b591d
- VIRTLET_CNI_CONFIG - json sting with CNI settings, ex: {"ip4":{"ip":"10.1.91.2/24","gateway":"10.1.91.1","routes":[{"dst":"0.0.0.0/0"}]},"dns":{}}
```
 - On CreateContainer request virlet calls libvirt api to start domain, which in its turn leads to running VMwrapper with all qemu args. Using info from set env vars VMWrapper sets up networking and runs dhcp-server inside pod's netns. VM itself is started inside virtlet's netns to provide inbound and outbound connectivity for qemu-kvm process.
In more details, VMWrapper inside pod's netns performs the following:
    1. creates tap0 for domain and br0, strips ip from veth end inside netns
    2. opens tap0 device and keeps its FD
    3. runs dhcp-server to pass ip to VM's eth stipped from veth and default routes

And at last VMWrapper spawns qemu-kvm inside virtlet's netns and provides it with FD of opened tap0.

**NOTE:** Currently we ignore hostNetwork setting, i.e. on RunPodSandBox request from kubelet new network namespace will be created by virtlet with name=PodId regardless of hostNetwork setting. As it's kubelet's work to decide when and which api request should be called, if hostNetwork setting will be changed for the running VM, kubelet SyncPod workflow will kill and re-create everything despite of fact that it won't change networking for VM.

In containers world hostNetwok=true means pods with such setting will have the same host ip and it's the responsibility of user then to watch for port overlapping of processes run inside containers.

In VM world we can't have two VM-pods with the same ip, so it means we need to have bridge binded to host interface for outbound VM's traffic (in other world the same as libvirt NAT-based network). But such model isin't sufficient for providing node-to-Pod connectivity for which we still need overlay network.

As a possible enhancement we could try to detect hostNetwork change setting case and emulate activity for kubelet not touching deployed VM (corresponding issue: https://github.com/Mirantis/virtlet/issues/184).
