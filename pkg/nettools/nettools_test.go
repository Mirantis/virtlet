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
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/davecgh/go-spew/spew"
	"github.com/vishvananda/netlink"
	"log"
	"net"
	"reflect"
	"testing"
)

var expectedInterfaceInfo = InterfaceInfo{
	IPNet: &net.IPNet{
		IP:   net.IP{10, 1, 90, 5},
		Mask: net.IPMask{255, 255, 255, 0},
	},
	Routes: []Route{
		{
			Destination: nil,
			Via:         net.IP{169, 254, 1, 1},
		},
		{
			Destination: &net.IPNet{
				IP:   net.IP{169, 254, 1, 1},
				Mask: net.IPMask{255, 255, 255, 255},
			},
			Via: nil,
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

func TestEscapePair(t *testing.T) {
	withHostAndContNS(t, func(hostNS, contNS ns.NetNS) {
		hostVeth, contVeth, err := CreateEscapeVethPair(contNS, "esc0", 1500)
		if err != nil {
			t.Fatalf("Error creating escape veth pair: %v", err)
		}
		// need to force hostNS here because of side effects of NetNS.Do()
		// See https://github.com/vishvananda/netns/issues/17
		hostNS.Do(func(ns.NetNS) error {
			if _, err = netlink.LinkByName(hostVeth.Attrs().Name); err != nil {
				t.Errorf("cannot locate host veth")
			}
			if _, err = netlink.LinkByName(contVeth.Attrs().Name); err == nil {
				t.Errorf("container veth should not be present in host namespace")
			}
			return nil
		})
		contNS.Do(func(ns.NetNS) error {
			if _, err = netlink.LinkByName(contVeth.Attrs().Name); err != nil {
				t.Errorf("cannot locate container veth")
			}
			if _, err = netlink.LinkByName(hostVeth.Attrs().Name); err == nil {
				t.Errorf("host veth should not be present in container namespace")
			}
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

func makeTestBridge(t *testing.T, links []netlink.Link) *netlink.Bridge {
	br, err := SetupBridge(links)
	if err != nil {
		t.Fatalf("failed to create first bridge: %v", err)
	}

	// re-retrieve link by name to be extra sure
	if _, err := netlink.LinkByName(br.Attrs().Name); err != nil {
		t.Fatalf("first bridge not found: %v", err)
	}

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
				makeTestBridge(t, links[0:2]),
				makeTestBridge(t, links[2:4]),
			}
			if brs[0].Attrs().Name == brs[1].Attrs().Name {
				t.Errorf("bridges have identical name %q", brs[0].Attrs().Name)
			}
			if brs[0].Attrs().Index == brs[1].Attrs().Index {
				t.Errorf("bridges have the same index %d", brs[0].Attrs().Index)
			}

			for i := 0; i < 4; i++ {
				bridgeIndex := brs[i/2].Attrs().Index
				name := links[i].Attrs().Name
				if link, err := netlink.LinkByName(name); err != nil {
					t.Errorf("cannot locate link %q", name)
				} else if link.Attrs().MasterIndex != bridgeIndex {
					t.Errorf("link %q doesn't belong to bridge %d", name, i/2)
				}
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

func TestExtractVethInfo(t *testing.T) {
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

			if info, err := GrabInterfaceInfo(); err != nil {
				t.Errorf("failed to grab interface info: %v", err)
			} else if !reflect.DeepEqual(*info, expectedInterfaceInfo) {
				t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
					spew.Sdump(expectedInterfaceInfo), spew.Sdump(*info))
			}

			if routes, err := netlink.RouteList(origContVeth, netlink.FAMILY_V4); err != nil {
				t.Errorf("failed to get route list: %v", err)
			} else if len(routes) != 0 {
				t.Errorf("unexpected routes remain on the interface: %s", spew.Sdump(routes))
			}

			if addrs, err := netlink.AddrList(origContVeth, netlink.FAMILY_V4); err != nil {
				t.Errorf("failed to get addresses for veth: %v", err)
			} else if len(addrs) != 0 {
				t.Errorf("unexpected addresses remain on the interface: %s", spew.Sdump(addrs))
			}

			return nil
		})
	})
}
