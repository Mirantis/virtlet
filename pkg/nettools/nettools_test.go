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

package nettools

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"reflect"
	"testing"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/vishvananda/netlink"
)

const (
	innerHwAddr       = "42:a4:a6:22:80:2e"
	outerHwAddr       = "42:b5:b7:33:91:3f"
	secondInnerHwAddr = "42:a4:a6:22:80:2f"
	secondOuterHwAddr = "42:b5:b7:33:91:3e"
	dummyInnerHwAddr  = "42:a4:a6:22:80:40"
	dummyOuterHwAddr  = "42:b5:b7:33:91:4f"
)

func expectedExtractedLinkInfo(contNsPath string) *cnicurrent.Result {
	return &cnicurrent.Result{
		Interfaces: []*cnicurrent.Interface{
			{
				Name:    "eth0",
				Mac:     innerHwAddr,
				Sandbox: contNsPath,
			},
		},
		IPs: []*cnicurrent.IPConfig{
			{
				Version:   "4",
				Interface: 0,
				Address: net.IPNet{
					IP:   net.IP{10, 1, 90, 5},
					Mask: net.IPMask{255, 255, 255, 0},
				},
				Gateway: net.IP{10, 1, 90, 1},
			},
		},
		Routes: []*cnitypes.Route{
			{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
				GW: net.IP{10, 1, 90, 1},
			},
		},
	}
}

func expectedExtractedCalicoLinkInfo(contNsPath string) *cnicurrent.Result {
	return &cnicurrent.Result{
		Interfaces: []*cnicurrent.Interface{
			{
				Name:    "eth0",
				Mac:     innerHwAddr,
				Sandbox: contNsPath,
			},
		},
		IPs: []*cnicurrent.IPConfig{
			{
				Version:   "4",
				Interface: 0,
				Address: net.IPNet{
					IP: net.IP{10, 1, 90, 5},
					// FIXME: actually, Calico uses
					// 255.255.255.255 as netmask here
					// (but we don't want to overcomplicate the test)
					Mask: net.IPMask{255, 255, 255, 0},
				},
				Gateway: net.IP{169, 254, 1, 1},
			},
		},
		Routes: []*cnitypes.Route{
			{
				Dst: net.IPNet{
					IP:   net.IP{169, 254, 1, 1},
					Mask: net.IPMask{255, 255, 255, 255},
				},
				GW: net.IP{0, 0, 0, 0},
			},
			{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
				GW: net.IP{169, 254, 1, 1},
			},
		},
	}
}

func addCalicoRoutes(t *testing.T, origContVeth netlink.Link) {
	addTestRoute(t, &netlink.Route{
		Dst:       parseAddr("169.254.1.1/32").IPNet,
		Scope:     SCOPE_LINK,
		LinkIndex: origContVeth.Attrs().Index,
	})
	addTestRoute(t, &netlink.Route{
		Gw:    parseAddr("169.254.1.1/32").IPNet.IP,
		Scope: SCOPE_UNIVERSE,
	})
}

// withTemporaryNSAvailable creates a new network namespace and
// passes is as an argument to the specified function.
// It does NOT change current network namespace.
func withTempNetNS(t *testing.T, toRun func(ns ns.NetNS)) {
	ns, err := ns.NewNS()
	if err != nil {
		t.Errorf("Error creating network namespace: %v", err)
		return
	}
	defer func() {
		// We use log.Panicf() instead of t.Fatalf() in these tests
		// because ns.Do() uses separate goroutine
		if r := recover(); r != nil {
			t.Fatal(r)
		}
		if err = ns.Close(); err != nil {
			t.Fatalf("Error closing network namespace: %v", err)
		}
	}()
	toRun(ns)
}

func inNS(netNS ns.NetNS, name string, toRun func()) {
	var r interface{}
	if err := netNS.Do(func(ns.NetNS) error {
		defer func() {
			//r = recover()
		}()
		toRun()
		return nil
	}); err != nil {
		log.Fatalf("failed to enter %s: %v", name, err)
	}

	// re-panic in the original goroutine
	if r != nil {
		log.Panic(r)
	}
}

// withHostAndContNS creates two namespaces, one serving as 'host'
// namespace and one serving as 'container' one, and calls
// the specified function in the 'host' namespace, passing both
// namespaces to it.
func withHostAndContNS(t *testing.T, toRun func(hostNS, contNS ns.NetNS)) {
	withTempNetNS(t, func(hostNS ns.NetNS) {
		withTempNetNS(t, func(contNS ns.NetNS) {
			inNS(hostNS, "hostNS", func() { toRun(hostNS, contNS) })
		})
	})
}

func verifyLinkUp(t *testing.T, name, title string) netlink.Link {
	link, err := netlink.LinkByName(name)
	if err != nil {
		log.Panicf("cannot locate link: %s", title)
	}
	if link.Attrs().Flags&net.FlagUp == 0 {
		t.Errorf("link should be up, but it's down: %s", title)
	}
	return link
}

func verifyNoLink(t *testing.T, name, title string) {
	if _, err := netlink.LinkByName(name); err == nil {
		t.Errorf("link should not be present: %s", title)
	}
}

func verifyBridgeMember(t *testing.T, name, title string, bridge netlink.Link) netlink.Link {
	if bridge.Type() != "bridge" {
		log.Panicf("link %q is not a bridge", bridge.Attrs().Name)
	}
	link := verifyLinkUp(t, name, title)
	if link.Attrs().MasterIndex != bridge.Attrs().Index {
		t.Errorf("link %q doesn't belong to bridge %q", name, bridge.Attrs().Name)
	}
	return link
}

func TestEscapePair(t *testing.T) {
	withHostAndContNS(t, func(hostNS, contNS ns.NetNS) {
		hostVeth, contVeth, err := CreateEscapeVethPair(contNS, "esc0", 1500)
		if err != nil {
			log.Panicf("Error creating escape veth pair: %v", err)
		}
		// need to force hostNS here because of side effects of NetNS.Do()
		// See https://github.com/vishvananda/netns/issues/17
		inNS(hostNS, "hostNS", func() {
			verifyLinkUp(t, hostVeth.Attrs().Name, "host veth")
			verifyNoLink(t, contVeth.Attrs().Name, "container veth in host namespace")
		})
		inNS(contNS, "contNS", func() {
			verifyLinkUp(t, contVeth.Attrs().Name, "container veth")
			verifyNoLink(t, hostVeth.Attrs().Name, "host veth in container namespace")
		})
	})
}

func makeTestVeth(t *testing.T, base string, index int) netlink.Link {
	name := fmt.Sprintf("%s%d", base, index)
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  name,
			Flags: net.FlagUp,
			MTU:   1500,
		},
		PeerName: "p" + name,
	}
	if err := netlink.LinkAdd(veth); err != nil {
		log.Panicf("failed to create veth: %v", err)
	}

	return veth
}

func makeTestBridge(t *testing.T, name string, links []netlink.Link) *netlink.Bridge {
	br, err := SetupBridge(name, links)
	if err != nil {
		log.Panicf("failed to create first bridge: %v", err)
	}
	if br.Attrs().Name != name {
		log.Panicf("bad bridge name: %q instead of %q", br.Attrs().Name, name)
	}
	verifyLinkUp(t, name, "bridge")
	return br
}

func TestSetupBridge(t *testing.T) {
	withTempNetNS(t, func(hostNS ns.NetNS) {
		inNS(hostNS, "hostNS", func() {
			var links []netlink.Link
			for i := 0; i < 4; i++ {
				links = append(links, makeTestVeth(t, "veth", i))
			}

			brs := []*netlink.Bridge{
				makeTestBridge(t, "testbr0", links[0:2]),
				makeTestBridge(t, "testbr1", links[2:4]),
			}
			if brs[0].Attrs().Name == brs[1].Attrs().Name {
				t.Errorf("bridges have identical name %q", brs[0].Attrs().Name)
			}
			if brs[0].Attrs().Index == brs[1].Attrs().Index {
				t.Errorf("bridges have the same index %d", brs[0].Attrs().Index)
			}

			for i := 0; i < 4; i++ {
				name := links[i].Attrs().Name
				verifyBridgeMember(t, name, name, brs[i/2])
			}
		})
	})
}

func parseAddr(addr string) *netlink.Addr {
	r, err := netlink.ParseAddr(addr)
	if err != nil {
		log.Panicf("failed to parse addr: %v", err)
	}
	return r
}

func addTestRoute(t *testing.T, route *netlink.Route) {
	if err := netlink.RouteAdd(route); err != nil {
		log.Panicf("Failed to add route %#v: %v", route, err)
	}
}

func setupLink(hwAddrAsText string, link netlink.Link) netlink.Link {
	hwAddr, err := net.ParseMAC(hwAddrAsText)
	if err != nil {
		log.Panicf("Error parsing hwaddr %q: %v", hwAddrAsText, err)
	}
	if err := SetHardwareAddr(link, hwAddr); err != nil {
		log.Panicf("Error setting hardware address: %v", err)
	}

	// re-query attrs (including new mac)
	link, err = netlink.LinkByName(link.Attrs().Name)
	if err != nil {
		log.Panicf("cannot locate link: %s", link.Attrs().Name)
	}

	return link
}

func withFakeCNIVeth(t *testing.T, mtu int, toRun func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link)) {
	withHostAndContNS(t, func(hostNS, contNS ns.NetNS) {
		origHostVeth, origContVeth, err := CreateEscapeVethPair(contNS, "eth0", mtu)
		if err != nil {
			log.Panicf("failed to create veth pair: %v", err)
		}
		// need to force hostNS here because of side effects of NetNS.Do()
		// See https://github.com/vishvananda/netns/issues/17
		inNS(hostNS, "hostNS", func() {
			origHostVeth = setupLink(outerHwAddr, origHostVeth)
		})
		inNS(contNS, "contNS", func() {
			origContVeth = setupLink(innerHwAddr, origContVeth)

			if err = netlink.AddrAdd(origContVeth, parseAddr("10.1.90.5/24")); err != nil {
				log.Panicf("failed to add addr for origContVeth: %v", err)
			}

			toRun(hostNS, contNS, origHostVeth, origContVeth)
		})
	})
}

func withFakeCNIVethAndGateway(t *testing.T, mtu int, toRun func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link)) {
	withFakeCNIVeth(t, mtu, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		addTestRoute(t, &netlink.Route{
			Gw:    parseAddr("10.1.90.1/24").IPNet.IP,
			Scope: SCOPE_UNIVERSE,
		})

		toRun(hostNS, contNS, origHostVeth, origContVeth)
	})
}

func verifyNoAddressAndRoutes(t *testing.T, link netlink.Link) {
	if routes, err := netlink.RouteList(link, FAMILY_V4); err != nil {
		t.Errorf("failed to get route list: %v", err)
	} else if len(routes) != 0 {
		t.Errorf("unexpected routes remain on the interface: %s", spew.Sdump(routes))
	}

	if addrs, err := netlink.AddrList(link, FAMILY_V4); err != nil {
		t.Errorf("failed to get addresses for veth: %v", err)
	} else if len(addrs) != 0 {
		t.Errorf("unexpected addresses remain on the interface: %s", spew.Sdump(addrs))
	}
}

func TestFindVeth(t *testing.T) {
	withFakeCNIVethAndGateway(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		allLinks, err := netlink.LinkList()
		if err != nil {
			log.Panicf("LinkList() failed: %v", err)
		}
		contVeth, err := FindVeth(allLinks)
		if err != nil {
			log.Panicf("FindVeth() failed: %v", err)
		}

		if contVeth.Attrs().Index != origContVeth.Attrs().Index {
			t.Errorf("GrabInterfaceInfo() didn't return original cont veth. Interface returned: %q", origContVeth.Attrs().Name)
		}
	})
}

func TestStripLink(t *testing.T) {
	withFakeCNIVethAndGateway(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			log.Panicf("StripLink() failed: %v", err)
		}
		verifyNoAddressAndRoutes(t, origContVeth)
	})
}

func TestExtractLinkInfo(t *testing.T) {
	withFakeCNIVethAndGateway(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		info, err := ExtractLinkInfo(origContVeth, contNS.Path())
		if err != nil {
			log.Panicf("failed to grab interface info: %v", err)
		}
		expectedInfo := expectedExtractedLinkInfo(contNS.Path())
		if !reflect.DeepEqual(info, expectedInfo) {
			t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
				spew.Sdump(expectedInfo), spew.Sdump(*info))
		}
	})
}

func TestExtractCalicoLinkInfo(t *testing.T) {
	withFakeCNIVeth(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		addCalicoRoutes(t, origContVeth)

		info, err := ExtractLinkInfo(origContVeth, contNS.Path())
		if err != nil {
			log.Panicf("failed to grab interface info: %v", err)
		}

		expectedInfo := expectedExtractedCalicoLinkInfo(contNS.Path())
		if !reflect.DeepEqual(info, expectedInfo) {
			t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
				spew.Sdump(expectedInfo), spew.Sdump(*info))
		}
	})
}

func verifyContainerSideNetwork(t *testing.T, origContVeth netlink.Link, contNsPath string, hostNS ns.NetNS, mtu int) {
	allLinks, err := netlink.LinkList()
	if err != nil {
		log.Panicf("error listing links: %v", err)
	}

	origHwAddr := origContVeth.Attrs().HardwareAddr
	expectedInfo := expectedExtractedLinkInfo(contNsPath)
	csn, err := SetupContainerSideNetwork(expectedInfo, contNsPath, allLinks, false, hostNS)
	if err != nil {
		log.Panicf("failed to set up container side network: %v", err)
	}
	expectedInfo = expectedExtractedLinkInfo(contNsPath)
	if !reflect.DeepEqual(csn.Result, expectedInfo) {
		t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
			spew.Sdump(expectedInfo), spew.Sdump(*csn.Result))
	}
	if !reflect.DeepEqual(origHwAddr, csn.Interfaces[0].HardwareAddr) {
		t.Errorf("bad hwaddr returned from SetupContainerSideNetwork: %v instead of %v", csn.Interfaces[0].HardwareAddr, origHwAddr)
	}
	// re-query origContVeth attrs
	origContVeth, err = netlink.LinkByName(origContVeth.Attrs().Name)
	if err != nil {
		log.Panicf("the original cni veth is gone")
	}
	if reflect.DeepEqual(origContVeth.Attrs().HardwareAddr, origHwAddr) {
		t.Errorf("cni veth hardware address didn't change")
	}
	if origContVeth.Attrs().MTU != mtu {
		t.Errorf("bad veth MTU: %d instead of %d", origContVeth.Attrs().MTU, mtu)
	}

	verifyNoAddressAndRoutes(t, origContVeth)

	bridge := verifyLinkUp(t, "br0", "in-container bridge")
	verifyBridgeMember(t, origContVeth.Attrs().Name, "origContVeth", bridge)
	tap := verifyBridgeMember(t, "tap0", "tap0", bridge)
	if tap.Type() != "tun" {
		t.Errorf("tap0 interface must have type tun, but has %q instead", tap.Type())
	}
	if tap.Attrs().MTU != mtu {
		t.Errorf("bad tap MTU: %d instead of %d", tap.Attrs().MTU, mtu)
	}

	addrs, err := netlink.AddrList(bridge, FAMILY_V4)
	if err != nil {
		t.Errorf("failed to get addresses for dhcp-side veth: %v", err)
	}
	expectedAddr := "169.254.254.2/24 br0"
	if len(addrs) != 1 {
		t.Errorf("br0 should have exactly one address, but got this instead: %v", spew.Sdump(addrs))
	} else if addrs[0].String() != expectedAddr {
		t.Errorf("bad br0 address %q (expected %q)", addrs[0].String(), expectedAddr)
	}

	if bridge.Attrs().MTU != mtu {
		t.Errorf("bad bridge MTU: %d instead of %d", bridge.Attrs().MTU, mtu)
	}
}

func TestSetUpContainerSideNetworkWithInfo(t *testing.T) {
	withFakeCNIVethAndGateway(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			log.Panicf("StripLink() failed: %v", err)
		}
		verifyContainerSideNetwork(t, origContVeth, contNS.Path(), hostNS, defaultMTU)
	})
}

func TestSetUpContainerSideNetworkMTU(t *testing.T) {
	withFakeCNIVethAndGateway(t, 9000, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			log.Panicf("StripLink() failed: %v", err)
		}
		verifyContainerSideNetwork(t, origContVeth, contNS.Path(), hostNS, 9000)
	})
}

func TestLoopbackInterface(t *testing.T) {
	withFakeCNIVethAndGateway(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		verifyContainerSideNetwork(t, origContVeth, contNS.Path(), hostNS, defaultMTU)
		if out, err := exec.Command("ping", "-c", "1", "127.0.0.1").CombinedOutput(); err != nil {
			log.Panicf("ping 127.0.0.1 failed:\n%s", out)
		}
	})
}

func stringInList(expected string, list []string) bool {
	for _, element := range list {
		if element == expected {
			return true
		}
	}

	return false
}

func verifyNoLinks(t *testing.T, linkNames []string) {
	links, err := netlink.LinkList()
	if err != nil {
		log.Panicf("netlink.LinkList failed: %v", err)
	}

	for _, link := range links {
		linkName := link.Attrs().Name
		if stringInList(linkName, linkNames) {
			t.Errorf("there should not be interface called %s in container namespace", linkName)
		}
	}
}

func verifyVethHaveConfiguration(t *testing.T, info *cnicurrent.Result) {
	allLinks, err := netlink.LinkList()
	if err != nil {
		log.Panicf("LinkList() failed: %v", err)
	}
	contVeth, err := FindVeth(allLinks)
	if err != nil {
		log.Panicf("FindVeth() failed: %v", err)
	}

	addrList, err := netlink.AddrList(contVeth, FAMILY_V4)
	if err != nil {
		log.Panicf("AddrList() failed: %v", err)
	}

	if len(addrList) != 1 {
		t.Errorf("veth should have single address but have: %d", len(addrList))
	}
	if !addrList[0].IP.Equal(info.IPs[0].Address.IP) {
		t.Errorf("veth has ip %s wherever expected is %s", addrList[0].IP.String(), info.IPs[0].Address.IP.String())
	}
	addrMaskSize := addrList[0].Mask.String()
	desiredMaskSize := info.IPs[0].Address.Mask.String()
	if addrMaskSize != desiredMaskSize {
		t.Errorf("veth has ipmask %s wherever expected is %s", addrMaskSize, desiredMaskSize)
	}

	routeList, err := netlink.RouteList(contVeth, FAMILY_V4)
	if err != nil {
		log.Panicf("RouteList() failed: %v", err)
	}

	for _, route := range routeList {
		for _, expRoute := range info.Routes {
			if route.Gw != nil {
				if route.Gw == nil && expRoute.GW.Equal(net.IPv4zero) {
					return
				}
				if route.Gw.String() == expRoute.GW.String() {
					return
				}
			}
		}
	}

	t.Errorf("not found desired route via: %s", info.Routes[0].GW.String())
}

func verifyTeardown(t *testing.T, hostNS, contNS ns.NetNS, origContVeth netlink.Link) {
	if err := StripLink(origContVeth); err != nil {
		log.Panicf("StripLink() failed: %v", err)
	}
	allLinks, err := netlink.LinkList()
	if err != nil {
		log.Panicf("error listing links: %v", err)
	}

	csn, err := SetupContainerSideNetwork(expectedExtractedCalicoLinkInfo(contNS.Path()), contNS.Path(), allLinks, false, hostNS)
	if err != nil {
		log.Panicf("failed to set up container side network: %v", err)
	}

	if err := Teardown(csn); err != nil {
		log.Panicf("failed to tear down container side network: %v", err)
	}

	verifyNoLinks(t, []string{"br0", "tap0"})
	verifyVethHaveConfiguration(t, expectedExtractedCalicoLinkInfo(contNS.Path()))

	// re-quiry origContVeth attrs
	origContVeth, err = netlink.LinkByName(origContVeth.Attrs().Name)
	if err != nil {
		log.Panicf("the original cni veth is gone")
	}
	if !reflect.DeepEqual(origContVeth.Attrs().HardwareAddr, csn.Interfaces[0].HardwareAddr) {
		t.Errorf("cni veth hardware address wasn't restored")
	}
}

func TestTeardownContainerSideNetwork(t *testing.T) {
	withFakeCNIVethAndGateway(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		verifyTeardown(t, hostNS, contNS, origContVeth)
	})
}

func TestTeardownCalico(t *testing.T) {
	withFakeCNIVeth(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		addCalicoRoutes(t, origContVeth)
		verifyTeardown(t, hostNS, contNS, origContVeth)
	})
}

func TestFindingLinkByAddress(t *testing.T) {
	withFakeCNIVeth(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		expectedInfo := expectedExtractedLinkInfo(contNS.Path())
		allLinks, err := netlink.LinkList()
		if err != nil {
			log.Panicf("LinkList() failed: %v", err)
		}

		link, err := findLinkByAddress(allLinks, expectedInfo.IPs[0].Address)
		if err != nil {
			t.Errorf("didn't found preconfigured link: %v", err)
		}
		if link == nil {
			t.Errorf("<nil> where configured link was expected")
		}

		link, err = findLinkByAddress(allLinks, *parseAddr("1.2.3.4/8").IPNet)
		if link != nil {
			t.Errorf("found link with dummy address")
		}
		if err == nil {
			t.Errorf("expected error but received <nil>")
		}
	})
}

func withMultipleInterfacesConfigured(t *testing.T, toRun func(contNS ns.NetNS, innerLinks []netlink.Link)) {
	withHostAndContNS(t, func(hostNS, contNS ns.NetNS) {
		var origContVeths [2]netlink.Link
		for n, vp := range []struct {
			name        string
			outerHwAddr string
			innerHwAddr string
			ip          string
		}{
			{"eth0", outerHwAddr, innerHwAddr, "10.1.90.5/24"},
			{"eth1", secondOuterHwAddr, secondInnerHwAddr, "192.168.37.8/16"},
		} {
			origHostVeth, origContVeth, err := CreateEscapeVethPair(contNS, vp.name, 1500)
			if err != nil {
				log.Panicf("failed to create veth pair %q: %v", vp.name, err)
			}
			inNS(hostNS, "hostNS", func() { setupLink(vp.outerHwAddr, origHostVeth) })
			inNS(contNS, "contNS", func() {
				origContVeths[n] = setupLink(vp.innerHwAddr, origContVeth)
				if err = netlink.AddrAdd(origContVeths[n], parseAddr(vp.ip)); err != nil {
					log.Panicf("failed to add addr for %q: %v", vp.name, err)
				}
			})
		}
		inNS(contNS, "contNS", func() {
			gwAddr := parseAddr("10.1.90.1/24")
			addTestRoute(t, &netlink.Route{
				Gw:    gwAddr.IPNet.IP,
				Scope: SCOPE_UNIVERSE,
			})

			toRun(contNS, origContVeths[:])
		})
	})
}

func expectedExtractedLinkInfoForMultipleInterfaces(contNsPath string) *cnicurrent.Result {
	expectedInfo := expectedExtractedLinkInfo(contNsPath)
	expectedInfo.IPs = append(expectedInfo.IPs, &cnicurrent.IPConfig{
		Version:   "4",
		Interface: 1,
		Address: net.IPNet{
			IP:   net.IP{192, 168, 37, 8},
			Mask: net.IPMask{255, 255, 0, 0},
		},
	})
	expectedInfo.Interfaces = append(expectedInfo.Interfaces, &cnicurrent.Interface{
		Name:    "eth1",
		Mac:     secondInnerHwAddr,
		Sandbox: contNsPath,
	})
	return expectedInfo
}

func expectedExtractedLinkInfoWithMissingInterface(contNsPath string) *cnicurrent.Result {
	expectedInfo := expectedExtractedLinkInfo(contNsPath)
	expectedInfo.IPs = append(expectedInfo.IPs, &cnicurrent.IPConfig{
		Version:   "4",
		Interface: -1,
		Address: net.IPNet{
			IP:   net.IP{192, 168, 37, 8},
			Mask: net.IPMask{255, 255, 0, 0},
		},
	})
	return expectedInfo
}

func TestMultiInterfaces(t *testing.T) {
	withMultipleInterfacesConfigured(t, func(contNS ns.NetNS, innerLinks []netlink.Link) {
		expectedInfo := expectedExtractedLinkInfoForMultipleInterfaces(contNS.Path())
		result, err := ValidateAndFixCNIResult(expectedInfo, contNS.Path(), innerLinks)
		if err != nil {
			t.Errorf("error during validate/fix cni result: %v", err)
		}
		if !reflect.DeepEqual(result, expectedInfo) {
			t.Errorf("result different than expected:\nActual:\n%s\nExpected:\n%s",
				spew.Sdump(result), spew.Sdump(expectedInfo))
		}
	})
}

func TestMultiInterfacesWithMissingInterface(t *testing.T) {
	withMultipleInterfacesConfigured(t, func(contNS ns.NetNS, innerLinks []netlink.Link) {
		infoToFix := expectedExtractedLinkInfoForMultipleInterfaces(contNS.Path())
		expectedInfo := expectedExtractedLinkInfoForMultipleInterfaces(contNS.Path())
		result, err := ValidateAndFixCNIResult(infoToFix, contNS.Path(), innerLinks)
		if err != nil {
			t.Errorf("error during validate/fix cni result: %v", err)
		}
		if !reflect.DeepEqual(result, expectedInfo) {
			t.Errorf("result different than expected:\nActual:\n%s\nExpected:\n%s",
				spew.Sdump(result), spew.Sdump(expectedInfo))
		}
	})
}
