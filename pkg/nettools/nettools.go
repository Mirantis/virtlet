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
	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/golang/glog"
	"github.com/vishvananda/netlink"
	"log"
	"net"
	"syscall"
)

const (
	tapInterfaceName         = "tap0"
	dhcpVethContainerEndName = "dhcpveth0"
	dhcpVethDhcpEndName      = "dhcpveth1"
	containerBridgeName      = "br0"
	// Address for dhcp server internal interface
	internalDhcpAddr = "169.254.254.2/24"
)

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
		log.Panicf("failed to parse address %q: %v", addr, err)
	}
	return r
}

// SetupContainerSideNetwork sets up networking in container namespace.
// It does so by calling GrabInterfaceInfo() first, then making additional
// network namespace for dhcp server and preparing the following network
// interfaces:
//   In container ns:
//     tap0      - tap interface for the VM
//     dhcpveth0 - container end of dchp veth pair
//     br0       - a bridge that joins tap0, dhcpveth0 and original CNI veth
//   In dchp server ns:
//     dhcpveth1 - dhcp server end of dhcp veth pair
// Returns container network desciption and error, if any
func SetupContainerSideNetwork(info *types.Result) (*ContainerNetwork, error) {
	contVeth, err := FindVeth()
	if err != nil {
		return nil, err
	}
	if info == nil {
		info, err = ExtractLinkInfo(contVeth)
		if err != nil {
			return nil, err
		}
	}
	if err := StripLink(contVeth); err != nil {
		return nil, err
	}

	keepNS := true
	dhcpNS, err := ns.NewNS()
	if err != nil {
		return nil, fmt.Errorf("failed to create dhcp namespace: %v", err)
	}
	defer func() {
		if keepNS {
			return
		}
		if err := dhcpNS.Close(); err != nil {
			glog.Errorf("failed to close dhcp ns: %v", err)
		}
	}()

	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			Name:  tapInterfaceName,
			Flags: net.FlagUp,
			MTU:   contVeth.Attrs().MTU,
		},
		Mode: netlink.TUNTAP_MODE_TAP,
	}
	if err := netlink.LinkAdd(tap); err != nil {
		return nil, fmt.Errorf("failed to create tap interface: %v", err)
	}
	if err = netlink.LinkSetUp(tap); err != nil {
		return nil, fmt.Errorf("failed to set %q up: %v", tapInterfaceName, err)
	}

	dhcpVeth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  dhcpVethContainerEndName,
			Flags: net.FlagUp,
			MTU:   contVeth.Attrs().MTU,
		},
		PeerName: dhcpVethDhcpEndName,
	}
	if err := netlink.LinkAdd(dhcpVeth); err != nil {
		return nil, fmt.Errorf("failed to create dhcp veth pair: %v", err)
	}
	if err = netlink.LinkSetUp(dhcpVeth); err != nil {
		return nil, fmt.Errorf("failed to set %q up: %v", dhcpVethContainerEndName, err)
	}

	dhcpSideVeth, err := netlink.LinkByName(dhcpVethDhcpEndName)
	if err != nil {
		return nil, fmt.Errorf("failed to find dhcp side veth before moving it: %v", err)
	}

	if err := netlink.LinkSetNsFd(dhcpSideVeth, int(dhcpNS.Fd())); err != nil {
		return nil, fmt.Errorf("failed to move dhcp side veth to its ns: %v", err)
	}

	if err := dhcpNS.Do(func(ns.NetNS) error {
		veth, err := netlink.LinkByName(dhcpVethDhcpEndName)
		if err != nil {
			return fmt.Errorf("failed to find dhcp side veth: %v", err)
		}
		if err = netlink.LinkSetUp(veth); err != nil {
			return fmt.Errorf("failed to set %q up: %v", dhcpVethDhcpEndName, err)
		}
		if err = netlink.AddrAdd(veth, mustParseAddr(internalDhcpAddr)); err != nil {
			return fmt.Errorf("failed to set address for dhcp side veth: %v", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if _, err := SetupBridge(containerBridgeName, []netlink.Link{contVeth, tap, dhcpVeth}); err != nil {
		return nil, fmt.Errorf("failed to create bridge: %v", err)
	}

	keepNS = true
	return &ContainerNetwork{
		Info:   info,
		DhcpNS: dhcpNS,
	}, nil
}
