package nettools

import (
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/vishvananda/netlink"
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
