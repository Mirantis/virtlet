/*
Copyright 2018 Mirantis

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
	"net"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/glog"
	"github.com/vishvananda/netlink"
)

var calicoGatewayIP = net.IP{169, 254, 1, 1}

// DetectCalico checks if the specified link in the current network
// namespace is configured by Calico. It returns two boolean values
// where the first one denotes whether Calico is used for the specified
// link and the second one denotes whether Calico's default route
// needs to be used. This approach is needed for multiple CNI use case
// when the types of individual CNI plugins are not available.
func DetectCalico(link netlink.Link) (bool, bool, error) {
	haveCalico, haveCalicoGateway := false, false
	routes, err := netlink.RouteList(link, FAMILY_V4)
	if err != nil {
		return false, false, fmt.Errorf("failed to list routes: %v", err)
	}
	for _, route := range routes {
		switch {
		case route.Protocol == RTPROT_KERNEL:
			// route created by kernel
		case route.LinkIndex == link.Attrs().Index && route.Gw == nil && route.Dst.IP.Equal(calicoGatewayIP):
			haveCalico = true
		case (route.Dst == nil || route.Dst.IP == nil) && route.Gw.Equal(calicoGatewayIP):
			haveCalicoGateway = true
		}
	}
	return haveCalico, haveCalico && haveCalicoGateway, nil
}

func getLinkForIPConfig(netConfig *cnicurrent.Result, ipConfigIndex int) (netlink.Link, error) {
	if ipConfigIndex > len(netConfig.IPs) {
		return nil, fmt.Errorf("ip config index out of range: %d", ipConfigIndex)
	}

	ipConfig := netConfig.IPs[ipConfigIndex]
	if ipConfig.Interface >= len(netConfig.Interfaces) {
		return nil, errors.New("interface index out of range in the CNI result")
	}

	if ipConfig.Version != "4" {
		return nil, errors.New("skipping non-IPv4 config")
	}

	iface := netConfig.Interfaces[ipConfig.Interface]
	if iface.Sandbox == "" {
		return nil, errors.New("error: IP config has non-sandboxed interface")
	}

	link, err := netlink.LinkByName(iface.Name)
	if err != nil {
		return nil, fmt.Errorf("can't get link %q: %v", iface.Name, err)
	}

	return link, nil
}

func getDummyGateway(dummyNetwork *cnicurrent.Result) (net.IP, error) {
	for n, ipConfig := range dummyNetwork.IPs {
		var haveCalico bool
		link, err := getLinkForIPConfig(dummyNetwork, n)
		if err == nil {
			haveCalico, _, err = DetectCalico(link)
		}
		if err != nil {
			glog.Warningf("Calico fix: dummy network: skipping link for config %d: %v", n, err)
			continue
		}
		if haveCalico {
			return ipConfig.Address.IP, nil
		}
	}
	return nil, errors.New("Calico fix: couldn't find dummy gateway")
}

// FixCalicoNetworking fixes Calico's CNI result in case if multiple
// CNIs are used, where route extraction in the container netns will
// not work properly. This is done by making sure Calico's
// link-scoped route to 169.254.1.1 and the default gateway
// are present in the CNI result.
// This function must be called from within the container network
// namespace.
func FixCalicoNetworking(netConfig *cnicurrent.Result) error {
	if len(netConfig.IPs) < 2 {
		// In case of single CNI, our route extraction should work
		return nil
	}

	for n, ipConfig := range netConfig.IPs {
		if ipConfig.Version != "4" {
			continue
		}

		link, err := getLinkForIPConfig(netConfig, n)
		if err != nil {
			glog.Warningf("Calico fix: skipping link for config %d: %v", n, err)
			continue
		}
		haveCalico, haveCalicoGateway, err := DetectCalico(link)
		if err != nil {
			return err
		}
		if !haveCalico {
			continue
		}

		ipConfig.Gateway = calicoGatewayIP[:]

		addDummyRoute := true
		addGw := haveCalicoGateway
		for _, r := range netConfig.Routes {
			if s, _ := r.Dst.Mask.Size(); s == 32 && r.Dst.IP.Equal(calicoGatewayIP) && r.GW.Equal(net.IPv4zero) {
				addDummyRoute = false
			} else if s, _ := r.Dst.Mask.Size(); s == 0 && r.GW.Equal(calicoGatewayIP) {
				addGw = false
			}
		}

		if addDummyRoute {
			netConfig.Routes = append(netConfig.Routes, &cnitypes.Route{
				Dst: net.IPNet{
					IP:   calicoGatewayIP[:],
					Mask: net.IPMask{255, 255, 255, 255},
				},
				GW: net.IP{0, 0, 0, 0},
			})
		}

		if addGw {
			netConfig.Routes = append(netConfig.Routes, &cnitypes.Route{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
				GW: calicoGatewayIP[:],
			})
		}
	}
	return nil
}
