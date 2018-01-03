/*
Copyright 2016 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Some of the code is based on CNI's plugins/main/bridge/bridge.go, pkg/ip/link.go
// Original copyright notice:
//
// Copyright 2014 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nettools

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/cni"
)

const (
	tapInterfaceNameTemplate    = "tap%d"
	containerBridgeNameTemplate = "br%d"
	loopbackInterfaceName       = "lo"
	// Address for dhcp server internal interface
	internalDhcpAddr = "169.254.254.2/24"

	SizeOfIfReq = 40
	IFNAMSIZ    = 16

	calicoDefaultSubnet = 24
	calicoSubnetVar     = "VIRTLET_CALICO_SUBNET"
)

// InterfaceType presents type of network interface instance
type InterfaceType int

const (
	InterfaceTypeTap InterfaceType = iota
	InterfaceTypeVF
)

// Had to duplicate ifReq here as it's not exported
type ifReq struct {
	Name  [IFNAMSIZ]byte
	Flags uint16
	pad   [SizeOfIfReq - IFNAMSIZ - 2]byte
}

type Route struct {
	Destination *net.IPNet
	Via         net.IP
}

type InterfaceInfo struct {
	IPNet  *net.IPNet
	Routes []Route
}

type ContainerNetwork struct {
	Info   *cnicurrent.Result
	DhcpNS ns.NetNS
}

func makeVethPair(name, peer string, mtu int) (netlink.Link, error) {
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  name,
			Flags: net.FlagUp,
			MTU:   mtu,
		},
		PeerName: peer,
	}
	if err := netlink.LinkAdd(veth); err != nil {
		return nil, err
	}

	return veth, nil
}

func peerExists(name string) bool {
	if _, err := netlink.LinkByName(name); err != nil {
		return false
	}
	return true
}

func makeVeth(name string, mtu int) (peerName string, veth netlink.Link, err error) {
	for i := 0; i < 10; i++ {
		peerName, err = RandomVethName()
		if err != nil {
			return
		}

		veth, err = makeVethPair(name, peerName, mtu)
		switch {
		case err == nil:
			return

		case os.IsExist(err):
			if peerExists(peerName) {
				continue
			}
			err = fmt.Errorf("container veth name provided (%v) already exists", name)
			return

		default:
			err = fmt.Errorf("failed to make veth pair: %v", err)
			return
		}
	}

	// should really never be hit
	err = fmt.Errorf("failed to find a unique veth name")
	return
}

// RandomVethName returns string "veth" with random prefix (hashed from entropy)
func RandomVethName() (string, error) {
	entropy := make([]byte, 4)
	_, err := rand.Reader.Read(entropy)
	if err != nil {
		return "", fmt.Errorf("failed to generate random veth name: %v", err)
	}

	// NetworkManager (recent versions) will ignore veth devices that start with "veth"
	return fmt.Sprintf("veth%x", entropy), nil
}

// SetupVeth sets up a pair of virtual ethernet devices.
// Call SetupVeth from inside the container netns.  It will create both veth
// devices and move the host-side veth into the provided hostNS namespace.
// On success, SetupVeth returns (hostVeth, containerVeth, nil)
func SetupVeth(contVethName string, mtu int, hostNS ns.NetNS) (netlink.Link, netlink.Link, error) {
	hostVethName, contVeth, err := makeVeth(contVethName, mtu)
	if err != nil {
		return nil, nil, err
	}

	if err = netlink.LinkSetUp(contVeth); err != nil {
		return nil, nil, fmt.Errorf("failed to set %q up: %v", contVethName, err)
	}

	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %q: %v", hostVethName, err)
	}

	if err = netlink.LinkSetNsFd(hostVeth, int(hostNS.Fd())); err != nil {
		return nil, nil, fmt.Errorf("failed to move veth to host netns: %v", err)
	}

	err = hostNS.Do(func(_ ns.NetNS) error {
		hostVeth, err = netlink.LinkByName(hostVethName)
		if err != nil {
			return fmt.Errorf("failed to lookup %q in %q: %v", hostVethName, hostNS.Path(), err)
		}

		if err = netlink.LinkSetUp(hostVeth); err != nil {
			return fmt.Errorf("failed to set %q up: %v", hostVethName, err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return hostVeth, contVeth, nil
}

// CreateEscapeVethPair creates a veth pair with innerVeth residing in
// the specified network namespace innerNS and outerVeth residing in
// the 'outer' (current) namespace.
// TBD: move this to test tools
func CreateEscapeVethPair(innerNS ns.NetNS, ifName string, mtu int) (outerVeth, innerVeth netlink.Link, err error) {
	var outerVethName string

	err = innerNS.Do(func(outerNS ns.NetNS) error {
		// create the veth pair in the inner ns and move outer end into the outer netns
		outerVeth, innerVeth, err = SetupVeth(ifName, mtu, outerNS)
		if err != nil {
			return err
		}

		// need to lookup innerVeth again to get its attrs
		innerVeth, err = netlink.LinkByName(innerVeth.Attrs().Name)
		if err != nil {
			return err
		}

		outerVethName = outerVeth.Attrs().Name
		return nil
	})
	if err != nil {
		return
	}

	// need to lookup outerVeth again as its index has changed during ns move
	outerVeth, err = netlink.LinkByName(outerVethName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %q: %v", outerVethName, err)
	}

	return
}

func createBridge(brName string, mtu int) (*netlink.Bridge, error) {
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: brName,
			MTU:  mtu,
			// Let kernel use default txqueuelen; leaving it unset
			// means 0, and a zero-length TX queue messes up FIFO
			// traffic shapers which use TX queue length as the
			// default packet limit
			TxQLen: -1,
		},
	}

	if err := netlink.LinkAdd(br); err != nil {
		return nil, fmt.Errorf("could not add %q: %v", brName, err)
	}

	if err := netlink.LinkSetUp(br); err != nil {
		return nil, err
	}

	return br, nil
}

// SetupBridge creates a bridge and adds specified links to it.
// It sets bridge's MTU to MTU value of the first link.
func SetupBridge(bridgeName string, links []netlink.Link) (*netlink.Bridge, error) {
	if len(links) == 0 {
		return nil, errors.New("no links provided")
	}

	br, err := createBridge(bridgeName, links[0].Attrs().MTU)
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge %q: %v", bridgeName, err)
	}

	for _, link := range links {
		if err = netlink.LinkSetMaster(link, br); err != nil {
			delMessage := ""
			if delErr := netlink.LinkDel(br); delErr != nil {
				delMessage = fmt.Sprintf(" (and failed to delete the bridge: %v", err)
			}
			return nil, fmt.Errorf("failed to connect %q to bridge %v: %v%s", link.Attrs().Name, br.Attrs().Name, err, delMessage)
		}
	}

	return br, nil
}

// FindVeth locates single veth link in the list of provided links.
// There must be exactly one veth interface in the list.
func FindVeth(links []netlink.Link) (netlink.Link, error) {
	var veth netlink.Link
	for _, link := range links {
		if link.Type() != "veth" {
			continue
		}
		if veth != nil {
			return nil, errors.New("multiple veth links detected in container namespace")
		}
		veth = link
	}
	if veth == nil {
		return nil, errors.New("no veth interface found")
	}
	return veth, nil
}

func findLinkByAddress(links []netlink.Link, address net.IPNet) (netlink.Link, error) {
	for _, link := range links {
		addresses, err := netlink.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return nil, err
		}
		for _, addr := range addresses {
			if addr.IPNet.String() == address.String() {
				return link, nil
			}
		}
	}
	return nil, fmt.Errorf("interface with address %q not found in container namespace", address.String())
}

// ValidateAndFixCNIResult verifies that netConfig contains proper list of
// ips, routes, interfaces and if something is missing it tries to complement
// that using patch for Weave or for plugins which return their netConfig
// in v0.2.0 version of CNI SPEC
func ValidateAndFixCNIResult(netConfig *cnicurrent.Result, nsPath string, allLinks []netlink.Link) (*cnicurrent.Result, error) {
	// If there are no routes provided, we consider it a broken
	// config and extract interface config instead. That's the
	// case with Weave CNI plugin. We don't do this for multiple CNI
	// at this point.
	if len(netConfig.IPs) == 1 && (cni.GetPodIP(netConfig) == "" || len(netConfig.Routes) == 0) {
		dnsInfo := netConfig.DNS

		veth, err := FindVeth(allLinks)
		if err != nil {
			return nil, err
		}
		if netConfig, err = ExtractLinkInfo(veth, nsPath); err != nil {
			return nil, err
		}

		// extracted netConfig doesn't have DNS information, so
		// still try to extract it from CNI-provided data
		netConfig.DNS = dnsInfo

		return netConfig, nil
	}

	if len(netConfig.IPs) == 0 {
		return nil, fmt.Errorf("cni result does not have any IP addresses")
	}

	// Interfaces contain broken info more often than not, so we
	// replace them here with what we can deduce from the network
	// links in the container netns
	for _, ipConfig := range netConfig.IPs {
		link, err := findLinkByAddress(allLinks, ipConfig.Address)
		if err != nil {
			return nil, err
		}

		found := false
		for i, iface := range netConfig.Interfaces {
			if iface.Name == link.Attrs().Name {
				ipConfig.Interface = i
				found = true
				break
			}
		}
		if !found {
			ipConfig.Interface = len(netConfig.Interfaces)
			netConfig.Interfaces = append(netConfig.Interfaces, &cnicurrent.Interface{
				Name:    link.Attrs().Name,
				Mac:     link.Attrs().HardwareAddr.String(),
				Sandbox: nsPath,
			})
		}
	}

	return netConfig, nil
}

// GetContainerLinks finds links that correspond to interfaces in the current
// network namespace
func GetContainerLinks(interfaces []*cnicurrent.Interface) ([]netlink.Link, error) {
	var links []netlink.Link
	for _, iface := range interfaces {
		// empty Sandbox means this interface belongs to the host
		// network namespace, so we skip it
		if iface.Sandbox == "" {
			continue
		}
		link, err := netlink.LinkByName(iface.Name)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

// StripLink removes addresses from the link
// along with any routes related to the link, except
// those created by the kernel
func StripLink(link netlink.Link) error {
	routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list routes: %v", err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to get addresses for link: %v", err)
	}

	for _, route := range routes {
		if route.Protocol == syscall.RTPROT_KERNEL {
			// route created by the kernel
			continue
		}
		if err = netlink.RouteDel(&route); err != nil {
			return fmt.Errorf("error deleting route: %v", err)
		}
	}

	for _, addr := range addrs {
		if err = netlink.AddrDel(link, &addr); err != nil {
			return fmt.Errorf("error deleting address from the route: %v", err)
		}
	}

	return nil
}

// ExtractLinkInfo extracts ip address and netmask from veth
// interface in the current namespace, together with routes for this
// interface.
// There must be exactly one veth interface in the namespace
// and exactly one address associated with veth.
// Returns interface info struct and error, if any.
func ExtractLinkInfo(link netlink.Link, nsPath string) (*cnicurrent.Result, error) {
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to get addresses for link: %v", err)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("expected exactly one address for link, but got %v", addrs)
	}

	result := &cnicurrent.Result{
		Interfaces: []*cnicurrent.Interface{
			{
				Name:    link.Attrs().Name,
				Mac:     link.Attrs().HardwareAddr.String(),
				Sandbox: nsPath,
			},
		},
		IPs: []*cnicurrent.IPConfig{
			{
				Version:   "4",
				Interface: 0,
				Address:   *addrs[0].IPNet,
			},
		},
	}

	routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %v", err)
	}
	for _, route := range routes {
		switch {
		case route.Protocol == syscall.RTPROT_KERNEL:
			// route created by kernel
		case route.Gw == nil:
			// these routes can't be represented properly
			// by CNI result because CNI will consider
			// them having IP's default Gateway value as
			// Gw
		case (route.Dst == nil || route.Dst.IP == nil) && route.Gw == nil:
			// route has only Src
		case (route.Dst == nil || route.Dst.IP == nil):
			result.IPs[0].Gateway = route.Gw
			result.Routes = append(result.Routes, &cnitypes.Route{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
				GW: route.Gw,
			})
		default:
			result.Routes = append(result.Routes, &cnitypes.Route{
				Dst: *route.Dst,
				GW:  route.Gw,
			})
		}
	}

	return result, nil
}

func mustParseAddr(addr string) *netlink.Addr {
	r, err := netlink.ParseAddr(addr)
	if err != nil {
		log.Panicf("Failed to parse address %q: %v", addr, err)
	}
	return r
}

func bringUpLoopback() error {
	// lo interface is already there in the new ns but it's down
	lo, err := netlink.LinkByName(loopbackInterfaceName)
	if err != nil {
		return fmt.Errorf("failed to find link %q: %v", loopbackInterfaceName, err)
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("failed to bring up link %q: %v", loopbackInterfaceName, err)
	}
	return nil
}

func updateEbTables(nsPath, interfaceName, command string) error {
	// block/unblock DHCP traffic from/to CNI-provided link
	for _, item := range []struct{ chain, opt string }{
		// dhcp responses originate from bridge itself
		{"OUTPUT", "--ip-source-port"},
		// dhcp requests originate from the VM
		{"FORWARD", "--ip-destination-port"},
	} {
		if out, err := exec.Command(
			"nsenter", "--net="+nsPath,
			"ebtables", command, item.chain, "-p", "IPV4", "--ip-protocol", "UDP",
			item.opt, "67", "--out-if", interfaceName, "-j", "DROP").CombinedOutput(); err != nil {
			return fmt.Errorf("[netns %q] ebtables failed: %v\nOut:\n%s", nsPath, err, out)
		}
	}

	return nil
}

func disableMacLearning(nsPath string, bridgeName string) error {
	if out, err := exec.Command("nsenter", "--net="+nsPath, "brctl", "setageing", bridgeName, "0").CombinedOutput(); err != nil {
		return fmt.Errorf("[netns %q] brctl failed: %v\nOut:\n%s", nsPath, err, out)
	}

	return nil
}

func SetHardwareAddr(link netlink.Link, hwAddr net.HardwareAddr) error {
	if err := netlink.LinkSetDown(link); err != nil {
		return fmt.Errorf("can't bring down the link: %v", err)
	}
	if err := netlink.LinkSetHardwareAddr(link, hwAddr); err != nil {
		return fmt.Errorf("can't set hardware address for the link: %v", err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("can't bring up the link: %v", err)
	}

	return nil
}

type InterfaceDescription struct {
	// Type contains interface type designator
	Type InterfaceType
	// Fo contains open File object pointing to tap device inside network
	// namespace or to control file in sysfs for sr-iov VF
	Fo *os.File
	// Name containes original interface name for sr-iov interface
	Name string
	// HardwareAddr contains original hardware address for CNI-created
	// veth link
	HardwareAddr net.HardwareAddr
	// PCIAddress contains a pci address for sr-iov vf interface
	PCIAddress string
	// MTU contains max transfer unit value for interface
	MTU uint16
}

// ContainerSideNetwork struct describes the container (VM) network
// namespace properties
type ContainerSideNetwork struct {
	// Result contains CNI result object describing the network settings
	Result *cnicurrent.Result
	// NsPath specifies the path to the container network namespace
	NsPath string
	// Interfaces contains a list of interfaces with data needed
	// to configure them
	Interfaces []InterfaceDescription
}

// verify if device is pci virtual function (in the same way as does
// that libvirt (src/util/virpci.c:virPCIIsVirtualFunction)
func isSriovVf(link netlink.Link) bool {
	_, err := os.Stat(filepath.Join("/sys/class/net", link.Attrs().Name, "device/physfn"))
	return err == nil
}

func openVfConfigFile(devName string) (*os.File, error) {
	return os.OpenFile(filepath.Join("/sys/bus/pci/devices", devName, "config"), os.O_RDWR, 0644)
}

func getPCIAddressOfVF(devName string) (string, error) {
	linkDestination, err := os.Readlink(filepath.Join("/sys/class/net", devName, "device"))
	if err != nil {
		return "", err
	}
	if linkDestination[:13] != "../../../0000" {
		return "", fmt.Errorf("unknown address as device symlink: %q", linkDestination)
	}
	// we need pci address without leading "../../../"
	return linkDestination[9:], nil
}

func getDevNameByPCIAddress(address string) (string, error) {
	desiredLinkLocation := "../../../" + address
	devices, err := ioutil.ReadDir("/sys/class/net")
	if err != nil {
		return "", err
	}
	for _, fi := range devices {
		linkDestination, err := os.Readlink(filepath.Join("/sys/class/net", fi.Name(), "device"))
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", err
		}
		if linkDestination == desiredLinkLocation {
			return fi.Name(), nil
		}
	}
	return "", fmt.Errorf("can't find network device with pci address %q", address)
}

func unbindDriverFromDevice(devName string) error {
	return ioutil.WriteFile(
		devName,
		[]byte(filepath.Join("/sys/bus/pci/devices", devName, "driver/unbind")),
		0200,
	)
}

func rebindDriverToDevice(devName string) error {
	return ioutil.WriteFile(
		devName,
		[]byte("/sys/bus/pci/drivers_probe"),
		0200,
	)
}

// SetupContainerSideNetwork sets up networking in container
// namespace.  It does so by preparing the following
// network interfaces in container ns:
//     tapX      - tap interface for the each interface to pass to VM
//     brX       - a bridge that joins above tapX and original CNI interface
// with X denoting an link index in info.Interfaces list.
// Each bridge gets assigned a link-local address to be used
// for dhcp server.
// In case of SR-IOV VFs this function only sets up a device to be passed to VM.
// The function should be called from within container namespace.
// Returns container network struct and an error, if any.
func SetupContainerSideNetwork(info *cnicurrent.Result, nsPath string, allLinks []netlink.Link) (*ContainerSideNetwork, error) {
	contLinks, err := GetContainerLinks(info.Interfaces)
	if err != nil {
		return nil, err
	}

	var interfaces []InterfaceDescription

	for i, link := range contLinks {
		hwAddr := link.Attrs().HardwareAddr
		ifaceName := link.Attrs().Name
		pciAddress := ""
		var ifaceType InterfaceType
		var fo *os.File

		mtu := link.Attrs().MTU

		if err := StripLink(link); err != nil {
			return nil, err
		}

		if isSriovVf(link) {
			if os.Getenv("VIRTLET_SRIOV_SUPPORT") == "" {
				return nil, fmt.Errorf("SR-IOV device configured in container network namespace while Virtlet is configured with disabled SR-IOV support")
			}

			ifaceType = InterfaceTypeVF

			pciAddress, err = getPCIAddressOfVF(ifaceName)
			if err != nil {
				return nil, err
			}

			fo, err = openVfConfigFile(pciAddress)
			if err != nil {
				return nil, err
			}

			if err := unbindDriverFromDevice(pciAddress); err != nil {
				return nil, err
			}

			glog.V(3).Infof("Adding interface %q as VF on %s address", ifaceName, pciAddress)
		} else {
			newHwAddr, err := GenerateMacAddress()
			if err == nil {
				err = SetHardwareAddr(link, newHwAddr)
			}
			if err != nil {
				return nil, err
			}

			ifaceType = InterfaceTypeTap

			tapInterfaceName := fmt.Sprintf(tapInterfaceNameTemplate, i)
			tap, err := CreateTAP(tapInterfaceName, mtu)
			if err != nil {
				return nil, err
			}

			containerBridgeName := fmt.Sprintf(containerBridgeNameTemplate, i)
			br, err := SetupBridge(containerBridgeName, []netlink.Link{link, tap})
			if err != nil {
				return nil, fmt.Errorf("failed to create bridge: %v", err)
			}

			if err := netlink.AddrAdd(br, mustParseAddr(internalDhcpAddr)); err != nil {
				return nil, fmt.Errorf("failed to set address for the bridge: %v", err)
			}

			// Add ebtables DHCP blocking rules
			if err := updateEbTables(nsPath, ifaceName, "-A"); err != nil {
				return nil, err
			}

			// Work around bridge MAC learning problem
			// https://ubuntuforums.org/showthread.php?t=2329373&s=cf580a41179e0f186ad4e625834a1d61&p=13511965#post13511965
			// (affects Flannel)
			if err := disableMacLearning(nsPath, containerBridgeName); err != nil {
				return nil, err
			}

			if err := bringUpLoopback(); err != nil {
				return nil, err
			}

			glog.V(3).Infof("Opening tap interface %q for link %q", tapInterfaceName, ifaceName)
			fo, err = OpenTAP(tapInterfaceName)
			if err != nil {
				return nil, fmt.Errorf("failed to open tap: %v", err)
			}
			glog.V(3).Infof("Adding interface %q as %q", ifaceName, tapInterfaceName)
		}

		interfaces = append(interfaces, InterfaceDescription{
			Type:         ifaceType,
			Name:         ifaceName,
			Fo:           fo,
			HardwareAddr: hwAddr,
			PCIAddress:   pciAddress,
			MTU:          uint16(mtu),
		})
	}

	return &ContainerSideNetwork{info, nsPath, interfaces}, nil
}

// RecreateContainerSideNetwork tries to populate ContainerSideNetwork
// structure based on a network namespace that was already adjusted for Virtlet
func RecreateContainerSideNetwork(info *cnicurrent.Result, nsPath string, allLinks []netlink.Link) (*ContainerSideNetwork, error) {
	if len(info.Interfaces) == 0 {
		return nil, fmt.Errorf("wrong cni configuration - missing interfaces list: %v", spew.Sdump(info))
	}

	// FIXME: this will not work with sr-iov device passed to VM
	contLinks, err := GetContainerLinks(info.Interfaces)
	if err != nil {
		return nil, err
	}

	var interfaces []InterfaceDescription

	for i, link := range contLinks {
		hwAddr := link.Attrs().HardwareAddr
		ifaceName := link.Attrs().Name
		pciAddress := ""
		var ifaceType InterfaceType
		var fo *os.File

		if isSriovVf(link) {
			ifaceType = InterfaceTypeVF
			pciAddress, err = getPCIAddressOfVF(ifaceName)
			if err != nil {
				return nil, err
			}

			fo, err = openVfConfigFile(pciAddress)
			if err != nil {
				return nil, err
			}

			// device should be already unbound, but after machine reboot that can be necessary
			_ = unbindDriverFromDevice(pciAddress)
		} else {
			ifaceType = InterfaceTypeTap
			tapInterfaceName := fmt.Sprintf(tapInterfaceNameTemplate, i)
			fo, err = OpenTAP(tapInterfaceName)
			if err != nil {
				return nil, fmt.Errorf("failed to open tap: %v", err)
			}
		}
		interfaces = append(interfaces, InterfaceDescription{
			Type:         ifaceType,
			Name:         ifaceName,
			Fo:           fo,
			HardwareAddr: hwAddr,
			PCIAddress:   pciAddress,
		})
	}

	return &ContainerSideNetwork{info, nsPath, interfaces}, nil
}

// TeardownBridge removes links from bridge and sets it down
func TeardownBridge(bridge netlink.Link, links []netlink.Link) error {
	for _, link := range links {
		if err := netlink.LinkSetNoMaster(link); err != nil {
			return err
		}
	}

	return netlink.LinkSetDown(bridge)
}

// ConfigureLink configures a link according to the CNI result
func ConfigureLink(link netlink.Link, info *cnicurrent.Result) error {
	ifaceNo := -1
	linkMAC := link.Attrs().HardwareAddr.String()
	for i, iface := range info.Interfaces {
		if iface.Mac == linkMAC {
			ifaceNo = i
			break
		}
	}
	if ifaceNo == -1 {
		return fmt.Errorf("can't find link with MAC %q in saved cni result: %s", linkMAC, spew.Sdump(info))
	}

	for _, addr := range info.IPs {
		if addr.Interface == ifaceNo {
			linkAddr := &netlink.Addr{IPNet: &addr.Address}
			if err := netlink.AddrAdd(link, linkAddr); err != nil {
				return fmt.Errorf("error adding address %v to link %q: %v", addr.Address, link.Attrs().Name, err)
			}

			for _, route := range info.Routes {
				// TODO: that's too naive - if there are more than one interfaces which have this gw address
				// in their subnet - same gw will be added on both of them
				// in theory this should be ok, but there is can lead to configuration other than prepared
				// by cni plugins
				if linkAddr.Contains(route.GW) {
					err := netlink.RouteAdd(&netlink.Route{
						LinkIndex: link.Attrs().Index,
						Scope:     netlink.SCOPE_UNIVERSE,
						Dst:       &route.Dst,
						Gw:        route.GW,
					})
					if err != nil {
						return fmt.Errorf("error adding route (dst %v gw %v): %v", route.Dst, route.GW, err)
					}
				}
			}
		}
	}

	return nil
}

// Teardown cleans up container network configuration.
// It does so by invoking teardown sequence which removes ebtables rules, links
// and addresses in an order opposite to that of their creation in SetupContainerSideNetwork.
// The end result is the same network configuration in the container network namespace
// as it was before SetupContainerSideNetwork() call.
func (csn *ContainerSideNetwork) Teardown() error {
	for _, i := range csn.Interfaces {
		i.Fo.Close()
	}

	contLinks, err := GetContainerLinks(csn.Result.Interfaces)
	if err != nil {
		return err
	}

	for i, contLink := range contLinks {
		// Remove ebtables DHCP rules
		if err := updateEbTables(csn.NsPath, contLink.Attrs().Name, "-D"); err != nil {
			return nil
		}

		if !isSriovVf(contLink) {
			tapInterfaceName := fmt.Sprintf(tapInterfaceNameTemplate, i)
			tap, err := netlink.LinkByName(tapInterfaceName)
			if err != nil {
				return err
			}

			containerBridgeName := fmt.Sprintf(containerBridgeNameTemplate, i)
			br, err := netlink.LinkByName(containerBridgeName)
			if err != nil {
				return err
			}

			if err := netlink.AddrDel(br, mustParseAddr(internalDhcpAddr)); err != nil {
				return err
			}

			if err := TeardownBridge(br, []netlink.Link{contLink, tap}); err != nil {
				return err
			}

			if err := netlink.LinkDel(br); err != nil {
				return err
			}

			if err := netlink.LinkSetDown(tap); err != nil {
				return err
			}

			if err := netlink.LinkDel(tap); err != nil {
				return err
			}

			if err := SetHardwareAddr(contLink, csn.Interfaces[i].HardwareAddr); err != nil {
				return err
			}
		}

		rereadLink, err := netlink.LinkByName(contLink.Attrs().Name)
		if err != nil {
			return err
		}
		if err := ConfigureLink(rereadLink, csn.Result); err != nil {
			return err
		}
	}

	return nil
}

// ReconstructVFs iterates over stored PCI addresses, rebinding each
// corresponding interface to its host driver, changing its MAC address
// to the stored value and then moving it into the container namespace
func (csn *ContainerSideNetwork) ReconstructVFs(netns ns.NetNS) error {
	for _, iface := range csn.Interfaces {
		if iface.Type != InterfaceTypeVF {
			continue
		}
		if err := rebindDriverToDevice(iface.PCIAddress); err != nil {
			return err
		}
		devName, err := getDevNameByPCIAddress(iface.PCIAddress)
		if err != nil {
			return err
		}
		link, err := netlink.LinkByName(devName)
		if err != nil {
			return fmt.Errorf("can't find link with name %q: %v", err)
		}
		if err := netlink.LinkSetHardwareAddr(link, iface.HardwareAddr); err != nil {
			return fmt.Errorf("can't set hwaddr %q on device %q: %v", iface.HardwareAddr, devName, err)
		}
		if err := netlink.LinkSetNsFd(link, int(netns.Fd())); err != nil {
			return fmt.Errorf("can't move link %q to netns %q: %v", iface.Name, netns.Path(), err)
		}
		if err := netns.Do(func(ns.NetNS) error {
			if link.Attrs().Name != iface.Name {
				if err := netlink.LinkSetName(link, iface.Name); err != nil {
					return fmt.Errorf("can't rename device %q to %q: %v", devName, iface.Name, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

// copied from:
// https://github.com/coreos/rkt/blob/56564bac090b44788684040f2ffd66463f29d5d0/stage1/init/kvm/network.go#L71
func GenerateMacAddress() (net.HardwareAddr, error) {
	mac := net.HardwareAddr{
		2,          // locally administred unicast
		0x65, 0x02, // OUI (randomly chosen by jell)
		0, 0, 0, // bytes to randomly overwrite
	}

	_, err := rand.Reader.Read(mac[3:6])
	if err != nil {
		return nil, fmt.Errorf("cannot generate random mac address: %v", err)
	}

	return mac, nil
}

var calicoGatewayIP = net.IP{169, 254, 1, 1}

// DetectCalico checks if the specified link in the current network
// namespace is configured by Calico. It returns two boolean values
// where the first one denotes whether Calico is used for the specified
// link and the second one denotes whether Calico's default route
// needs to be used. This approach is needed for multiple CNI use case
// when the types of individual CNI plugins are not available.
func DetectCalico(link netlink.Link) (bool, bool, error) {
	haveCalico, haveCalicoGateway := false, false
	routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		return false, false, fmt.Errorf("failed to list routes: %v", err)
	}
	for _, route := range routes {
		switch {
		case route.Protocol == syscall.RTPROT_KERNEL:
			// route created by kernel
		case route.LinkIndex == link.Attrs().Index && route.Gw == nil && route.Dst.IP.Equal(calicoGatewayIP):
			haveCalico = true
		case (route.Dst == nil || route.Dst.IP == nil) && route.Gw.Equal(calicoGatewayIP):
			haveCalicoGateway = true
		}
	}
	return haveCalico, haveCalico && haveCalicoGateway, nil
}

func getLinkForIPConfig(netConfig *cnicurrent.Result, ipConfigIndex int) (netlink.Link, error) {
	if ipConfigIndex > len(netConfig.IPs) {
		return nil, fmt.Errorf("ip config index out of range: %d", ipConfigIndex)
	}

	ipConfig := netConfig.IPs[ipConfigIndex]
	if ipConfig.Interface >= len(netConfig.Interfaces) {
		return nil, errors.New("interface index out of range in the CNI result")
	}

	if ipConfig.Version != "4" {
		return nil, errors.New("skipping non-IPv4 config")
	}

	iface := netConfig.Interfaces[ipConfig.Interface]
	if iface.Sandbox == "" {
		return nil, errors.New("error: IP config has non-sandboxed interace")
	}

	link, err := netlink.LinkByName(iface.Name)
	if err != nil {
		return nil, fmt.Errorf("can't get link %q: %v", iface.Name, err)
	}

	return link, nil
}

func getDummyGateway(dummyNetwork *cnicurrent.Result) (net.IP, error) {
	for n, ipConfig := range dummyNetwork.IPs {
		var haveCalico bool
		link, err := getLinkForIPConfig(dummyNetwork, n)
		if err == nil {
			haveCalico, _, err = DetectCalico(link)
		}
		if err != nil {
			glog.Warningf("Calico fix: dummy network: skipping link for config %d: %v", n, err)
			continue
		}
		if haveCalico {
			return ipConfig.Address.IP, nil
		}
	}
	return nil, errors.New("Calico fix: couldn't find dummy gateway")
}

// FixCalicoNetworking updates netConfig to make Calico work with
// Virtlet's DHCP-server based scheme. It does so by throwing away
// Calico's gateway and dev route and using a fake gateway instead.
// The fake gateway provided by getDummyGateway() is just an IP
// address allocated by Calico IPAM, it's needed for proper ARP
// responses for VMs.
// This function must be called from within the container network
// namespace.
func FixCalicoNetworking(netConfig *cnicurrent.Result, getDummyNetwork func() (*cnicurrent.Result, string, error)) error {
	for n, ipConfig := range netConfig.IPs {
		link, err := getLinkForIPConfig(netConfig, n)
		if err != nil {
			glog.Warningf("Calico fix: skipping link for config %d: %v", n, err)
			continue
		}
		haveCalico, haveCalicoGateway, err := DetectCalico(link)
		if err != nil {
			return err
		}
		if !haveCalico {
			continue
		}
		ipConfig.Address.Mask = netmaskForCalico()
		if haveCalicoGateway {
			dummyNetwork, nsPath, err := getDummyNetwork()
			if err != nil {
				return err
			}
			dummyNS, err := ns.GetNS(nsPath)
			if err != nil {
				return err
			}
			if err := dummyNS.Do(func(ns.NetNS) error {
				allLinks, err := netlink.LinkList()
				if err != nil {
					return fmt.Errorf("failed to list links inside the dummy netns: %v", err)
				}
				dummyNetwork, err := ValidateAndFixCNIResult(dummyNetwork, nsPath, allLinks)
				if err != nil {
					return err
				}
				dummyGateway, err := getDummyGateway(dummyNetwork)
				if err != nil {
					return err
				}
				ipConfig.Gateway = dummyGateway
				return nil
			}); err != nil {
				return err
			}

			var newRoutes []*cnitypes.Route
			// remove the default gateway
			for _, r := range netConfig.Routes {
				if r.Dst.Mask != nil {
					ones, _ := r.Dst.Mask.Size()
					if ones == 0 {
						continue
					}
				}
				newRoutes = append(newRoutes, r)
			}
			netConfig.Routes = append(newRoutes, &cnitypes.Route{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
				GW: ipConfig.Gateway,
			})
		}
	}
	return nil
}

func netmaskForCalico() net.IPMask {
	n := calicoDefaultSubnet
	subnetStr := os.Getenv(calicoSubnetVar)
	if subnetStr != "" {
		n, err := strconv.Atoi(subnetStr)
		if err != nil || n <= 0 || n > 30 {
			glog.Warningf("bad calico subnet %q, using /%d", subnetStr, calicoDefaultSubnet)
			n = calicoDefaultSubnet
		}
	}
	return net.CIDRMask(n, 32)
}
