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
	"reflect"
	"testing"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/davecgh/go-spew/spew"
	"github.com/vishvananda/netlink"
)

var expectedExtractedLinkInfo = types.Result{
	IP4: &types.IPConfig{
		IP: net.IPNet{
			IP:   net.IP{10, 1, 90, 5},
			Mask: net.IPMask{255, 255, 255, 0},
		},
		Gateway: net.IP{169, 254, 1, 1},
		Routes: []types.Route{
			{
				Dst: net.IPNet{
					IP:   net.IP{169, 254, 1, 1},
					Mask: net.IPMask{255, 255, 255, 255},
				},
				GW: nil,
			},
		},
	},
}

// withTemporaryNSAvailable creates a new network namespace and
// passes is as an argument to the specified function.
// It does NOT change current network namespace.
func withTempNetNS(t *testing.T, toRun func(ns ns.NetNS)) {
	ns, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Error creating network namespace: %v", err)
	}
	defer func() {
		if err = ns.Close(); err != nil {
			t.Fatalf("Error closing network namespace: %v", err)
		}
	}()
	toRun(ns)
}

// withHostAndContNS creates two namespaces, one serving as 'host'
// namespace and one serving as 'container' one, and calls
// the specified function in the 'host' namespace, passing both
// namespaces to it.
func withHostAndContNS(t *testing.T, toRun func(hostNS, contNS ns.NetNS)) {
	withTempNetNS(t, func(hostNS ns.NetNS) {
		withTempNetNS(t, func(contNS ns.NetNS) {
			hostNS.Do(func(ns.NetNS) error {
				toRun(hostNS, contNS)
				return nil
			})
		})
	})
}

func verifyLinkUp(t *testing.T, name, title string) netlink.Link {
	link, err := netlink.LinkByName(name)
	if err != nil {
		t.Fatalf("cannot locate link: %s", title)
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
		t.Fatalf("link %q is not a bridge", bridge.Attrs().Name)
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
			t.Fatalf("Error creating escape veth pair: %v", err)
		}
		// need to force hostNS here because of side effects of NetNS.Do()
		// See https://github.com/vishvananda/netns/issues/17
		hostNS.Do(func(ns.NetNS) error {
			verifyLinkUp(t, hostVeth.Attrs().Name, "host veth")
			verifyNoLink(t, contVeth.Attrs().Name, "container veth in host namespace")
			return nil
		})
		contNS.Do(func(ns.NetNS) error {
			verifyLinkUp(t, contVeth.Attrs().Name, "container veth")
			verifyNoLink(t, hostVeth.Attrs().Name, "host veth in container namespace")
			return nil
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
		t.Fatalf("failed to create veth: %v", err)
	}

	return veth
}

func makeTestBridge(t *testing.T, name string, links []netlink.Link) *netlink.Bridge {
	br, err := SetupBridge(name, links)
	if err != nil {
		t.Fatalf("failed to create first bridge: %v", err)
	}
	if br.Attrs().Name != name {
		t.Fatalf("bad bridge name: %q instead of %q", br.Attrs().Name, name)
	}
	verifyLinkUp(t, name, "bridge")
	return br
}

func TestSetupBridge(t *testing.T) {
	withTempNetNS(t, func(hostNS ns.NetNS) {
		hostNS.Do(func(ns.NetNS) error {
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

			return nil
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
		t.Fatalf("Failed to add route %#v: %v", route, err)
	}
}

func withFakeCNIVeth(t *testing.T, toRun func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link)) {
	withHostAndContNS(t, func(hostNS, contNS ns.NetNS) {
		origHostVeth, origContVeth, err := CreateEscapeVethPair(contNS, "eth0", 1500)
		if err != nil {
			t.Fatalf("failed to create veth pair: %v", err)
		}
		// need to force hostNS here because of side effects of NetNS.Do()
		// See https://github.com/vishvananda/netns/issues/17
		hostNS.Do(func(ns.NetNS) error {
			if err = netlink.LinkSetUp(origHostVeth); err != nil {
				t.Fatalf("failed to bring up origHostVeth: %v", err)
			}
			return nil
		})
		contNS.Do(func(ns.NetNS) error {
			if err = netlink.LinkSetUp(origContVeth); err != nil {
				t.Fatalf("failed to bring up origContVeth: %v", err)
			}
			if err = netlink.AddrAdd(origContVeth, parseAddr("10.1.90.5/24")); err != nil {
				t.Fatalf("failed to add addr for origContVeth: %v", err)
			}

			gwAddr := parseAddr("169.254.1.1/32")
			addTestRoute(t, &netlink.Route{
				LinkIndex: origContVeth.Attrs().Index,
				Dst:       gwAddr.IPNet,
				Scope:     netlink.SCOPE_LINK,
			})

			addTestRoute(t, &netlink.Route{
				LinkIndex: origContVeth.Attrs().Index,
				Gw:        gwAddr.IPNet.IP,
				Scope:     netlink.SCOPE_UNIVERSE,
			})

			toRun(hostNS, contNS, origHostVeth, origContVeth)

			return nil
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
		contVeth, err := FindVeth()
		if err != nil {
			t.Fatalf("FindVeth() failed: %v", err)
		}

		if contVeth.Attrs().Index != origContVeth.Attrs().Index {
			t.Errorf("GrabInterfaceInfo() didn't return original cont veth. Interface returned: %q", origContVeth.Attrs().Name)
		}
	})
}

func TestStripLink(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			t.Fatalf("StripLink() failed: %v", err)
		}
		verifyNoAddressAndRoutes(t, origContVeth)
	})
}

func TestExtractLinkInfo(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		info, err := ExtractLinkInfo(origContVeth)
		if err != nil {
			t.Fatalf("failed to grab interface info: %v", err)
		}
		if !reflect.DeepEqual(*info, expectedExtractedLinkInfo) {
			t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
				spew.Sdump(expectedExtractedLinkInfo), spew.Sdump(*info))
		}
	})
}

func verifyContainerSideNetwork(t *testing.T, origContVeth netlink.Link, info *types.Result) {
	containerNetwork, err := SetupContainerSideNetwork(info)
	if err != nil {
		t.Fatalf("failed to set up container side network: %v", err)
	}
	defer containerNetwork.DhcpNS.Close()
	if !reflect.DeepEqual(*containerNetwork.Info, expectedExtractedLinkInfo) {
		t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
			spew.Sdump(expectedExtractedLinkInfo), spew.Sdump(*containerNetwork.Info))
	}
	verifyNoAddressAndRoutes(t, origContVeth)

	bridge := verifyLinkUp(t, "br0", "in-container bridge")
	verifyBridgeMember(t, origContVeth.Attrs().Name, "origContVeth", bridge)
	tap := verifyBridgeMember(t, "tap0", "tap0", bridge)
	if tap.Type() != "tun" {
		t.Errorf("tap0 interface must have type tun, but has %q instead", tap.Type())
	}
	dhcpContainerSideVeth := verifyBridgeMember(t, "dhcpveth0", "dhcpveth0", bridge)
	if dhcpContainerSideVeth.Type() != "veth" {
		t.Errorf("dhcpveth0 interface must have type veth, but has %q instead", dhcpContainerSideVeth.Type())
	}
	containerNetwork.DhcpNS.Do(func(ns.NetNS) error {
		link := verifyLinkUp(t, "dhcpveth1", "dhcp server veth")
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			t.Errorf("failed to get addresses for dhcp-side veth: %v", err)
		}
		expectedAddr := "169.254.254.2/24 dhcpveth1"
		if len(addrs) != 1 {
			t.Errorf("dhcp-side veth should have exactly one address, but got this instead: %v", spew.Sdump(addrs))
		} else if addrs[0].String() != expectedAddr {
			t.Errorf("bad dhcp-side veth address %q (expected %q)", addrs[0].String(), expectedAddr)
		}

		return nil
	})
}

func TestSetUpContainerSideNetwork(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		verifyContainerSideNetwork(t, origContVeth, nil)
	})
}

func TestSetUpContainerSideNetworkWithInfo(t *testing.T) {
	withFakeCNIVeth(t, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
		if err := StripLink(origContVeth); err != nil {
			t.Fatalf("StripLink() failed: %v", err)
		}
		verifyContainerSideNetwork(t, origContVeth, &expectedExtractedLinkInfo)
	})
}
