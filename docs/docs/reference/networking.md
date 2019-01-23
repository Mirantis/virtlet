# Virtlet networking principles

Virtlet currently uses [CNI-based](https://github.com/containernetworking/cni)
networking, which means you must use `--network-plugin=cni` for kubelet on
Virtlet nodes.  Virtlet should work with any CNI plugin which returns correct
[CNI Result](https://github.com/containernetworking/cni/blob/spec-v0.3.1/SPEC.md#result)
according to the CNI protocol specification.  The CNI spec version in use must
be 0.3.0 at least.

Virtlet expects the CNI plugins to provide
[veth](http://man7.org/linux/man-pages/man4/veth.4.html) or
[SR-IOV](https://en.wikipedia.org/wiki/Single-root_input/output_virtualization)
interfaces inside the network namespace.

For each veth inside the network namespace Virtlet sets up
a [TAP](https://en.wikipedia.org/wiki/TUN/TAP) interface that's passed to the
hypervisor process ([QEMU](https://www.qemu.org)) and a bridge that's used to
connect veth with the TAP interface.  The bridge is also used by Virtlet's
internal DHCP server which passes the IP configuration to the VM.

Each SR-IOV device available in the network namespace will be removed from host
visibility and passed to the hypervisor via PCI host passthrough mechanism.
Any [VLAN IDs](https://en.wikipedia.org/wiki/Virtual_LAN) set up by the plugin
still apply.

# Supported CNI implementations

Virtlet is verified to work correctly with the following CNI implementations:

* [Flannel](https://github.com/coreos/flannel)
* [Calico](https://github.com/projectcalico/cni-plugin)
* [Weave](https://github.com/weaveworks/weave)
* [original](https://github.com/hustcat/sriov-cni) and [Intel fork](https://github.com/intel/sriov-cni) of SR-IOV
* [combination](#multi-cni) of them with [CNI-Genie](https://github.com/Huawei-PaaS/CNI-Genie)

Virtlet may or may not work with CNI implementations that aren't listed here.

# DHCP-based VM network configuration

The network namespace configuration provided by the CNI plugin(s) in use is
passed to the VM using Virtlet's internal DHCP server.  The DHCP server is
started for each CNI-configured network interface, except for [SR-IOV](#sr-iov)
interfaces.  The DHCP server can only talk to the VM and is not accessible
from the cluster network.

DHCP server isn't used for SR-IOV devices as they're passed directly to the VM
while their links are removed from host.

# Configuring using Cloud-Init

Besides using DHCP server, Virtlet can also pass the network configuration as
a part of [Cloud-init](./cloud-init.md) data.  This is the preferred way of
setting up SR-IOV devices.  The network configuration is added to the
cloud-init user-data using
[Network Config Version 1](https://cloudinit.readthedocs.io/en/latest/topics/network-config-format-v1.html)format.

Note: Cloud-init network configuration is not supported for persistent rootfs
for now.

# <a name="multi-cni"></a> Setting up Multiple CNIs

Virtlet allows to configure multiple interfaces for VM when all of them are
properly described in
[CNI Result](https://github.com/containernetworking/cni/blob/spec-v0.3.1/SPEC.md#result).
The supported way to achieve that using CNI plugins is by combining
output of their chain using [CNI-Genie](https://github.com/Huawei-PaaS/CNI-Genie).

Before you proceed, please read the CNI Genie [documentation](https://github.com/Huawei-PaaS/CNI-Genie/blob/master/docs/CNIGenieFeatureSet.md).
There are two ways to tell CNI Genie which CNI networks to use:

* by setting `cni: <plugins list>` in the pod annotation
* by setting the `default_plugin` option in the CNI Genie configuration file

Note: when using Calico plugin, you must specify it as the first one in the
plugin chain, or, alternatively, disable the default route setup for other
plugins that precede Calico.  This is a Calico-specific limitation.

## Configuring networks

CNI plugins are expected to use 0.3.0 version of CNI Result spec or later.
Each CNI config must include `cniVersion` field, with minimum version being
0.3.0, too.

## Sample configuration

Please refer to the [detailed documentation](https://github.com/Mirantis/virtlet/blob/master/docs/multiple-interfaces.md#example-files)
that contains an example of configuration files for CNI Genie with Calico being used for
the primary interface and Flannel being used for the secondary one.

# SR-IOV

Any SR-IOV devices contained in the CNI result are passed to the VM using PCI
host-passthrough.  The hardware configuration which is set up by the CNI plugin
(MAC address, VLAN tag) is preserved by Virtlet.  If a VLAN ID is set it's
configured on the host side, so it can't be changed from within the VM to gain
unauthorised network access.
