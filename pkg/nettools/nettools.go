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
	"log"
	"net"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

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

func OpenTAP(devName string) (*os.File, error) {
	tapFile, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	var req ifReq

	// set IFF_NO_PI to not provide packet information
	// If flag IFF_NO_PI is not set each frame format is:
	// Flags [2 bytes]
	// Proto [2 bytes]
	// Raw protocol ethernet frame.
	// This extra 4-byte header breaks connectivity as in this case kernel truncates initial package
	req.Flags = uint16(syscall.IFF_TAP | syscall.IFF_NO_PI | syscall.IFF_ONE_QUEUE)
	copy(req.Name[:15], devName)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tapFile.Fd(), uintptr(syscall.TUNSETIFF), uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return nil, fmt.Errorf("tuntap IOCTL TUNSETIFF failed, errno %v", errno)
	}
	return tapFile, nil
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
func ValidateAndFixCNIResult(netConfig *cnicurrent.Result, podNs string) (*cnicurrent.Result, error) {
	allLinks, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("error listing links: %v", err)
	}

	// If there are no routes provided, we consider it a broken
	// config and extract interface config instead. That's the
	// case with Weave CNI plugin.
	if cni.GetPodIP(netConfig) == "" || len(netConfig.Routes) == 0 {
		dnsInfo := netConfig.DNS

		veth, err := FindVeth(allLinks)
		if err != nil {
			return nil, err
		}
		if netConfig, err = ExtractLinkInfo(veth, podNs); err != nil {
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

	// If on list of interfaces are missing elements matching these mentioned
	// by interface index in elements of ip list and for all elements
	// of this list which have value -1 for interface index - add them to list
	// of interfaces and fix its index in ip list entry
	if len(netConfig.Interfaces) == 0 {
		alreadyDefindeLinks, err := GetContainerLinks(netConfig.Interfaces)
		if err != nil {
			return nil, err
		}

		for _, ipConfig := range netConfig.IPs {
			link, err := findLinkByAddress(allLinks, ipConfig.Address)
			if err != nil {
				return nil, err
			}

			found := false
			for i, l := range alreadyDefindeLinks {
				if l == link {
					ipConfig.Interface = i
					found = true
					break
				}
			}
			if !found {
				netConfig.Interfaces = append(netConfig.Interfaces, &cnicurrent.Interface{
					Name:    link.Attrs().Name,
					Mac:     link.Attrs().HardwareAddr.String(),
					Sandbox: podNs,
				})
				ipConfig.Interface = len(alreadyDefindeLinks)
				alreadyDefindeLinks = append(alreadyDefindeLinks, link)
			}
		}
	}

	return netConfig, nil
}

func findLinkByName(links []netlink.Link, name string) (netlink.Link, error) {
	for _, link := range links {
		if link.Attrs().Name == name {
			return link, nil
		}
	}
	return nil, fmt.Errorf("interface with name %q not found in container namespace", name)
}

// GetContainerLinks locates in container namespac enetwork links
// for provided interfaces
func GetContainerLinks(interfaces []*cnicurrent.Interface) ([]netlink.Link, error) {
	allLinks, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("error listing links: %v", err)
	}

	var links []netlink.Link
	for _, iface := range interfaces {
		if iface.Sandbox == "" {
			continue
		}
		link, err := findLinkByName(allLinks, iface.Name)
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

// ContainerSideNetwork struct describes the container (VM) network
// namespace properties
type ContainerSideNetwork struct {
	// Result contains CNI result object describing the network settings
	Result *cnicurrent.Result
	// NsPath specifies the path to the container network namespace
	NsPath string
	// TapFiles contains a list of open File objects pointing to tap
	// devices inside the network namespace
	TapFiles []*os.File
	// HardwareAddrs contains a list of original hardware addresses of
	// CNI-created veth links
	HardwareAddrs []net.HardwareAddr
}

// SetupContainerSideNetwork sets up networking in container
// namespace.  It does so by preparing the following
// network interfaces in container ns:
//     tapX      - tap interface for the each interface to pass to VM
//     brX       - a bridge that joins above tapX and original CNI interface
// with X denoting an link index in info.Interfaces list.
// Each bridge gets assigned a link-local address to be used
// for dhcp server.
// The function should be called from within container namespace.
// Returns container network struct and an error, if any
func SetupContainerSideNetwork(info *cnicurrent.Result, nsPath string) (*ContainerSideNetwork, error) {
	contLinks, err := GetContainerLinks(info.Interfaces)
	if err != nil {
		return nil, err
	}

	var (
		tapFiles []*os.File
		hwAddrs  []net.HardwareAddr
	)

	for i, link := range contLinks {
		hwAddr := link.Attrs().HardwareAddr
		newHwAddr, err := GenerateMacAddress()
		if err == nil {
			err = SetHardwareAddr(link, newHwAddr)
		}
		if err == nil {
			err = StripLink(link)
		}
		if err != nil {
			return nil, err
		}

		tapInterfaceName := fmt.Sprintf(tapInterfaceNameTemplate, i)
		tap := &netlink.Tuntap{
			LinkAttrs: netlink.LinkAttrs{
				Name:  tapInterfaceName,
				Flags: net.FlagUp,
				MTU:   link.Attrs().MTU,
			},
			Mode: netlink.TUNTAP_MODE_TAP,
		}
		if err := netlink.LinkAdd(tap); err != nil {
			return nil, fmt.Errorf("failed to create tap interface: %v", err)
		}

		if err := netlink.LinkSetUp(tap); err != nil {
			return nil, fmt.Errorf("failed to set %q up: %v", tapInterfaceName, err)
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
		if err := updateEbTables(nsPath, link.Attrs().Name, "-A"); err != nil {
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

		glog.V(3).Infof("Opening tap interface %q for link %q", tapInterfaceName, link.Attrs().Name)
		tapFile, err := OpenTAP(tapInterfaceName)
		if err != nil {
			return nil, fmt.Errorf("failed to open tap: %v", err)
		}

		hwAddrs = append(hwAddrs, hwAddr)
		tapFiles = append(tapFiles, tapFile)
	}

	return &ContainerSideNetwork{info, nsPath, tapFiles, hwAddrs}, nil
}

// RecreateContainerSideNetwork tries to populate ContainerSideNetwork
// structure based on a network namespace that was already adjusted for Virtlet
func RecreateContainerSideNetwork(info *cnicurrent.Result, nsPath string) (*ContainerSideNetwork, error) {
	if len(info.Interfaces) == 0 {
		return nil, fmt.Errorf("wrong cni configuration - missing interfaces list: %v", spew.Sdump(info))
	}

	var (
		tapFiles []*os.File
		hwAddrs  []net.HardwareAddr
	)

	for i, iface := range info.Interfaces {
		if iface.Sandbox == "" {
			continue
		}
		hwAddr, err := net.ParseMAC(iface.Mac)
		if err != nil {
			return nil, fmt.Errorf("invalid mac address %q: %v", iface.Mac, err)
		}

		tapInterfaceName := fmt.Sprintf(tapInterfaceNameTemplate, i)
		tapFile, err := OpenTAP(tapInterfaceName)
		if err != nil {
			return nil, fmt.Errorf("failed to open tap: %v", err)
		}

		hwAddrs = append(hwAddrs, hwAddr)
		tapFiles = append(tapFiles, tapFile)
	}

	return &ContainerSideNetwork{info, nsPath, tapFiles, hwAddrs}, nil
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

// ConfigureLink adds to link ip address and routes based on info.
func ConfigureLink(link netlink.Link, info *cnicurrent.Result) error {
	linkNo := -1
	linkMAC := link.Attrs().HardwareAddr.String()
	for i, iface := range info.Interfaces {
		if iface.Mac == linkMAC {
			linkNo = i
			break
		}
	}
	if linkNo == -1 {
		return fmt.Errorf("can not find link with MAC %q in saved cni result: %s", linkMAC, spew.Sdump(info))
	}

	for _, addr := range info.IPs {
		if addr.Interface == linkNo {
			addr := &netlink.Addr{IPNet: &addr.Address}
			if err := netlink.AddrAdd(link, addr); err != nil {
				return err
			}

			for _, route := range info.Routes {
				// TODO: that's too naive - if there are more than one interfaces which have this gw address
				// in their subnet - same gw will be added on both of them
				// in theory this should be ok, but there is can lead to configuration other than prepared
				// by cni plugins
				if addr.Contains(route.GW) {
					err := netlink.RouteAdd(&netlink.Route{
						LinkIndex: link.Attrs().Index,
						Scope:     netlink.SCOPE_UNIVERSE,
						Dst:       &route.Dst,
						Gw:        route.GW,
					})
					if err != nil {
						return err
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
	for _, f := range csn.TapFiles {
		f.Close()
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

		if err := SetHardwareAddr(contLink, csn.HardwareAddrs[i]); err != nil {
			return err
		}

		rereadedLink, err := netlink.LinkByName(contLink.Attrs().Name)
		if err != nil {
			return err
		}
		if err := ConfigureLink(rereadedLink, csn.Result); err != nil {
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
