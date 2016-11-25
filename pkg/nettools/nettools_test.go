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
	"github.com/vishvananda/netlink"
	"net"
	"testing"
)

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
		if _, err = netlink.LinkByName(hostVeth.Attrs().Name); err != nil {
			t.Errorf("cannot locate host veth")
		}
		if _, err = netlink.LinkByName(contVeth.Attrs().Name); err == nil {
			t.Errorf("container veth should not be present in host namespace")
		}
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
