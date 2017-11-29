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
	innerHwAddr = "42:a4:a6:22:80:2e"
	outerHwAddr = "42:b5:b7:33:91:3f"
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

func withFakeCNIVeth(t *testing.T, toRun func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link)) {
	withHostAndContNS(t, func(hostNS, contNS ns.NetNS) {
		origHostVeth, origContVeth, err := CreateEscapeVethPair(contNS, "eth0", 1500)
		if err != nil {
			log.Panicf("failed to create veth pair: %v", err)
		}
		// need to force hostNS here because of side effects of NetNS.Do()
		// See https://github.com/vishvananda/netns/issues/17
		inNS(hostNS, "hostNS", func() {
			hwAddr, err := net.ParseMAC(outerHwAddr)
			if err != nil {
				log.Panicf("Error parsing hwaddr %q: %v", hwAddr, err)
			}
			err = SetHardwareAddr(origHostVeth, hwAddr)

			// re-query attrs (including new mac)
			origHostVeth, err = netlink.LinkByName(origHostVeth.Attrs().Name)
			if err != nil {
				log.Panicf("cannot locate link: %s", origHostVeth.Attrs().Name)
			}

			if err = netlink.LinkSetUp(origHostVeth); err != nil {
				log.Panicf("failed to bring up origHostVeth: %v", err)
			}
		})
		inNS(contNS, "contNS", func() {
			hwAddr, err := net.ParseMAC(innerHwAddr)
			if err != nil {
				log.Panicf("Error parsing hwaddr %q: %v", hwAddr, err)
			}
			err = SetHardwareAddr(origContVeth, hwAddr)

			// re-query attrs (including new mac)
			origContVeth, err = netlink.LinkByName(origContVeth.Attrs().Name)
			if err != nil {
				log.Panicf("cannot locate link: %s", origContVeth.Attrs().Name)
			}

			if err = netlink.LinkSetUp(origContVeth); err != nil {
				log.Panicf("failed to bring up origContVeth: %v", err)
			}
			if err = netlink.AddrAdd(origContVeth, parseAddr("10.1.90.5/24")); err != nil {
				log.Panicf("failed to add addr for origContVeth: %v", err)
			}

			gwAddr := parseAddr("10.1.90.1/24")

			addTestRoute(t, &netlink.Route{
				LinkIndex: origContVeth.Attrs().Index,
				Gw:        gwAddr.IPNet.IP,
				Scope:     netlink.SCOPE_UNIVERSE,
			})

			toRun(hostNS, contNS, origHostVeth, origContVeth)
		})
	})
}

func verifyNoAddressAndRoutes(t *testing.T, link netlink.Link) {
	if routes, err := netlink.RouteList(link, netlink.FAMILY_V4); err != nil {
		t.Errorf("failed to get route list: %v", err)
	} else if len(routes) != 0 {
		t.Errorf("unexpected routes remain on the interface: %s", spew.Sdump(routes))
	}

	if addrs, err := netlink.AddrList(link, netlink.FAMILY_V4); err != nil {
		t.Errorf("failed to get addresses for veth: %v", err)
	} else if len(addrs) != 0 {
		t.Errorf("unexpected addresses remain on the interface: %s", spew.Sdump(addrs))
	}
}

func TestFindVeth(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		allLinks, err := netlink.LinkList()
		if err != nil {
			log.Panic("LinkList() failed: %v", err)
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
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			log.Panicf("StripLink() failed: %v", err)
		}
		verifyNoAddressAndRoutes(t, origContVeth)
	})
}

func TestExtractLinkInfo(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
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

func verifyContainerSideNetwork(t *testing.T, origContVeth netlink.Link, contNsPath string) {
	origHwAddr := origContVeth.Attrs().HardwareAddr
	expectedInfo := expectedExtractedLinkInfo(contNsPath)
	csn, err := SetupContainerSideNetwork(expectedInfo, contNsPath)
	if err != nil {
		log.Panicf("failed to set up container side network: %v", err)
	}
	expectedInfo = expectedExtractedLinkInfo(contNsPath)
	if !reflect.DeepEqual(csn.Result, expectedInfo) {
		t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
			spew.Sdump(expectedInfo), spew.Sdump(*csn.Result))
	}
	if !reflect.DeepEqual(origHwAddr, csn.HardwareAddrs[0]) {
		t.Errorf("bad hwaddr returned from SetupContainerSideNetwork: %v instead of %v", csn.HardwareAddrs[0], origHwAddr)
	}
	// re-query origContVeth attrs
	origContVeth, err = netlink.LinkByName(origContVeth.Attrs().Name)
	if err != nil {
		log.Panicf("the original cni veth is gone")
	}
	if reflect.DeepEqual(origContVeth.Attrs().HardwareAddr, origHwAddr) {
		t.Errorf("cni veth hardware address didn't change")
	}

	verifyNoAddressAndRoutes(t, origContVeth)

	bridge := verifyLinkUp(t, "br0", "in-container bridge")
	verifyBridgeMember(t, origContVeth.Attrs().Name, "origContVeth", bridge)
	tap := verifyBridgeMember(t, "tap0", "tap0", bridge)
	if tap.Type() != "tun" {
		t.Errorf("tap0 interface must have type tun, but has %q instead", tap.Type())
	}

	addrs, err := netlink.AddrList(bridge, netlink.FAMILY_V4)
	if err != nil {
		t.Errorf("failed to get addresses for dhcp-side veth: %v", err)
	}
	expectedAddr := "169.254.254.2/24 br0"
	if len(addrs) != 1 {
		t.Errorf("br0 should have exactly one address, but got this instead: %v", spew.Sdump(addrs))
	} else if addrs[0].String() != expectedAddr {
		t.Errorf("bad br0 address %q (expected %q)", addrs[0].String(), expectedAddr)
	}
}

func TestSetUpContainerSideNetworkWithInfo(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			log.Panicf("StripLink() failed: %v", err)
		}
		verifyContainerSideNetwork(t, origContVeth, contNS.Path())
	})
}

func TestLoopbackInterface(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		verifyContainerSideNetwork(t, origContVeth, contNS.Path())
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
		log.Panic("LinkList() failed: %v", err)
	}
	contVeth, err := FindVeth(allLinks)
	if err != nil {
		log.Panicf("FindVeth() failed: %v", err)
	}

	addrList, err := netlink.AddrList(contVeth, netlink.FAMILY_V4)
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

	routeList, err := netlink.RouteList(contVeth, netlink.FAMILY_V4)
	if err != nil {
		log.Panicf("RouteList() failed: %v", err)
	}

	for _, route := range routeList {
		if route.Gw != nil {
			if route.Gw.String() == info.Routes[0].GW.String() {
				return
			}
		}
	}

	t.Errorf("not found desired route to: %s", info.Routes[0].GW.String())
}

func TestTeardownContainerSideNetwork(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			log.Panicf("StripLink() failed: %v", err)
		}
		csn, err := SetupContainerSideNetwork(expectedExtractedLinkInfo(contNS.Path()), contNS.Path())
		if err != nil {
			log.Panicf("failed to set up container side network: %v", err)
		}

		if err := csn.Teardown(); err != nil {
			log.Panicf("failed to tear down container side network: %v", err)
		}

		verifyNoLinks(t, []string{"br0", "tap0"})
		verifyVethHaveConfiguration(t, expectedExtractedLinkInfo(contNS.Path()))

		// re-quiry origContVeth attrs
		origContVeth, err = netlink.LinkByName(origContVeth.Attrs().Name)
		if err != nil {
			log.Panicf("the original cni veth is gone")
		}
		if !reflect.DeepEqual(origContVeth.Attrs().HardwareAddr, csn.HardwareAddrs[0]) {
			t.Errorf("cni veth hardware address wasn't restored")
		}
	})
}
