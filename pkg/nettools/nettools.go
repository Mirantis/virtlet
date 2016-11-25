package nettools

import (
	"fmt"
	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/vishvananda/netlink"
)

// CreateEscapeVethPair creates a veth pair with contVeth residing in
// the specified container network namespace and hostVeth residing in
// the host namespace.
func CreateEscapeVethPair(contNS ns.NetNS, ifName string, mtu int) (hostVeth, contVeth netlink.Link, err error) {
	var hostVethName string

	err = contNS.Do(func(hostNS ns.NetNS) error {
		// create the veth pair in the container and move host end into host netns
		hostVeth, contVeth, err = ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}

		hostVethName = hostVeth.Attrs().Name
		return nil
	})
	if err != nil {
		return
	}

	// need to lookup hostVeth again as its index has changed during ns move
	hostVeth, err = netlink.LinkByName(hostVethName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %q: %v", hostVethName, err)
	}

	return
}
