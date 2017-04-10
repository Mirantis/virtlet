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
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/vishvananda/netlink"
)

const (
	tapInterfaceName      = "tap0"
	containerBridgeName   = "br0"
	loopbackInterfaceName = "lo"
	// Address for dhcp server internal interface
	internalDhcpAddr = "169.254.254.2/24"

	SizeOfIfReq = 40
	IFNAMSIZ    = 16
)

// Had to duplicate ifReq here as it's not  exported
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
	Info   *types.Result
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
	copy(req.Name[:15], "tap0")
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tapFile.Fd(), uintptr(syscall.TUNSETIFF), uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return nil, fmt.Errorf("Tuntap IOCTL TUNSETIFF failed, errno %v", errno)
	}
	return tapFile, nil
}

// CreateEscapeVethPair creates a veth pair with innerVeth residing in
// the specified network namespace innerNS and outerVeth residing in
// the 'outer' (current) namespace.
// TBD: move this to test tools
func CreateEscapeVethPair(innerNS ns.NetNS, ifName string, mtu int) (outerVeth, innerVeth netlink.Link, err error) {
	var outerVethName string

	err = innerNS.Do(func(outerNS ns.NetNS) error {
		// create the veth pair in the inner ns and move outer end into the outer netns
		outerVeth, innerVeth, err = ip.SetupVeth(ifName, mtu, outerNS)
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

// FindVeth locates veth link in the current network namespace.
// There must be exactly one veth interface in the namespace.
func FindVeth() (netlink.Link, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("error listing links: %v", err)
	}
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
func ExtractLinkInfo(link netlink.Link) (*types.Result, error) {
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to get addresses for link: %v", err)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("expected exactly one address for link, but got %v", addrs)
	}

	result := &types.Result{
		IP4: &types.IPConfig{
			IP: *addrs[0].IPNet,
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
			result.IP4.Gateway = route.Gw
			result.IP4.Routes = append(result.IP4.Routes, types.Route{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
			})
		default:
			result.IP4.Routes = append(result.IP4.Routes, types.Route{
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

func updateEbTables(interfaceName, command string) error {
	// block/unblock DHCP traffic from/to CNI-provided link
	for _, item := range []struct{ chain, opt string }{
		// dhcp responses originate from bridge itself
		{"OUTPUT", "--ip-source-port"},
		// dhcp requests originate from the VM
		{"FORWARD", "--ip-destination-port"},
	} {
		if out, err := exec.Command("ebtables", command, item.chain, "-p", "IPV4", "--ip-protocol", "UDP",
			item.opt, "67", "--out-if", interfaceName, "-j", "DROP").CombinedOutput(); err != nil {
			return fmt.Errorf("ebtables failed: %v\nOut:\n%s", err, out)
		}
	}

	return nil
}

// SetupContainerSideNetwork sets up networking in container
// namespace.  It does so by calling ExtractLinkInfo() first unless
// non-nil info argument is provided and then preparing the following
// network interfaces in container ns:
//     tap0      - tap interface for the VM
//     br0       - a bridge that joins tap0 and original CNI veth
// The bridge (br0) gets assigned a link-local address to be used
// for dhcp server.
// The function should be called from within container namespace.
// Returns container network info (CNI Result) and error, if any
func SetupContainerSideNetwork(info *types.Result) (*types.Result, *os.File, error) {
	contVeth, err := FindVeth()
	if err != nil {
		return nil, nil, err
	}
	if info == nil {
		info, err = ExtractLinkInfo(contVeth)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := StripLink(contVeth); err != nil {
		return nil, nil, err
	}

	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			Name:  tapInterfaceName,
			Flags: net.FlagUp,
			MTU:   contVeth.Attrs().MTU,
		},
		Mode: netlink.TUNTAP_MODE_TAP,
	}
	if err := netlink.LinkAdd(tap); err != nil {
		return nil, nil, fmt.Errorf("failed to create tap interface: %v", err)
	}

	if err := netlink.LinkSetUp(tap); err != nil {
		return nil, nil, fmt.Errorf("failed to set %q up: %v", tapInterfaceName, err)
	}

	br, err := SetupBridge(containerBridgeName, []netlink.Link{contVeth, tap})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create bridge: %v", err)
	}

	if err := netlink.AddrAdd(br, mustParseAddr(internalDhcpAddr)); err != nil {
		return nil, nil, fmt.Errorf("failed to set address for the bridge: %v", err)
	}

	// Add ebtables DHCP blocking rules
	if err := updateEbTables(contVeth.Attrs().Name, "-A"); err != nil {
		return nil, nil, err
	}

	if err := bringUpLoopback(); err != nil {
		return nil, nil, err
	}

	tapFile, err := OpenTAP(tapInterfaceName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open tap: %v", err)
	}

	return info, tapFile, nil
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
func ConfigureLink(link netlink.Link, info *types.Result) error {
	if err := netlink.AddrAdd(link, mustParseAddr(info.IP4.IP.String())); err != nil {
		return err
	}

	for _, route := range info.IP4.Routes {
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

	return nil
}

// TeardownContainerSideNetwork cleans up container network configuration.
// It does so by invoking teardown sequence which removes ebtables rules, links
// and addresses in an order opposite to that of their creation in SetupContainerSideNetwork.
// The end result is the same network configuration in the container network namespace
// as it was before SetupContainerSideNetwork() call.
func TeardownContainerSideNetwork(info *types.Result) error {
	contVeth, err := FindVeth()
	if err != nil {
		return err
	}

	// Remove ebtables DHCP rules
	if err := updateEbTables(contVeth.Attrs().Name, "-D"); err != nil {
		return nil
	}

	tap, err := netlink.LinkByName(tapInterfaceName)
	if err != nil {
		return err
	}

	br, err := netlink.LinkByName(containerBridgeName)
	if err != nil {
		return err
	}

	if err := netlink.AddrDel(br, mustParseAddr(internalDhcpAddr)); err != nil {
		return err
	}

	if err := TeardownBridge(br, []netlink.Link{contVeth, tap}); err != nil {
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

	return ConfigureLink(contVeth, info)
}

// copied from:
// https://github.com/coreos/rkt/blob/56564bac090b44788684040f2ffd66463f29d5d0/stage1/init/kvm/network.go#L71
func GenerateMacAddress() (net.HardwareAddr, error) {
	mac := net.HardwareAddr{
		2,          // locally administred unicast
		0x65, 0x02, // OUI (randomly chosen by jell)
		0, 0, 0, // bytes to randomly overwrite
	}

	_, err := rand.Read(mac[3:6])
	if err != nil {
		return nil, fmt.Errorf("cannot generate random mac address: %v", err)
	}

	return mac, nil
}
