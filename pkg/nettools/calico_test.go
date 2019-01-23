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

package nettools

import (
	"log"
	"net"
	"reflect"
	"testing"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/vishvananda/netlink"
)

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

func withDummyNetworkNamespace(t *testing.T, toRun func(dummyNS ns.NetNS, dummyInfo *cnicurrent.Result)) {
	withHostAndContNS(t, func(hostNS, contNS ns.NetNS) {
		origHostVeth, origContVeth, err := CreateEscapeVethPair(contNS, "eth0", 1500)
		if err != nil {
			log.Panicf("failed to create veth pair: %v", err)
		}
		// need to force hostNS here because of side effects of NetNS.Do()
		// See https://github.com/vishvananda/netns/issues/17
		inNS(hostNS, "hostNS", func() {
			origHostVeth = setupLink(outerHwAddr, origHostVeth)
		})
		var dummyInfo *cnicurrent.Result
		inNS(contNS, "contNS", func() {
			origContVeth = setupLink(innerHwAddr, origContVeth)

			if err = netlink.AddrAdd(origContVeth, parseAddr("10.1.90.100/24")); err != nil {
				log.Panicf("failed to add addr for origContVeth: %v", err)
			}

			addCalicoRoutes(t, origContVeth)

			dummyInfo, err = ExtractLinkInfo(origContVeth, contNS.Path())
			if err != nil {
				log.Panicf("failed to grab dummy interface info: %v", err)
			}
		})
		toRun(contNS, dummyInfo)
	})

}

func TestCalicoDetection(t *testing.T) {
	for _, tc := range []struct {
		name              string
		routes            []netlink.Route
		haveCalico        bool
		haveCalicoGateway bool
	}{
		{
			name:              "no routes",
			haveCalico:        false,
			haveCalicoGateway: false,
		},
		{
			name: "non-calico default route",
			routes: []netlink.Route{
				{
					// LinkIndex: origContVeth.Attrs().Index,
					Gw:    parseAddr("10.1.90.1/24").IPNet.IP,
					Scope: SCOPE_UNIVERSE,
				},
			},
			haveCalico:        false,
			haveCalicoGateway: false,
		},
		{
			name: "calico w/o default gw",
			routes: []netlink.Route{
				{
					Dst:   parseAddr("169.254.1.1/32").IPNet,
					Scope: SCOPE_LINK,
				},
			},
			haveCalico:        true,
			haveCalicoGateway: false,
		},
		{
			name: "calico with default gw",
			routes: []netlink.Route{
				{
					Dst:   parseAddr("169.254.1.1/32").IPNet,
					Scope: SCOPE_LINK,
				},
				{
					Gw:    parseAddr("169.254.1.1/32").IPNet.IP,
					Scope: SCOPE_UNIVERSE,
				},
			},
			haveCalico:        true,
			haveCalicoGateway: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withFakeCNIVeth(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
				for _, r := range tc.routes {
					if r.Scope == SCOPE_LINK {
						r.LinkIndex = origContVeth.Attrs().Index
					}
					addTestRoute(t, &r)
				}
				haveCalico, haveCalicoGateway, err := DetectCalico(origContVeth)
				if err != nil {
					log.Panicf("DetectCalico(): %v", err)
				}
				if haveCalico != tc.haveCalico {
					log.Panicf("haveCalico is expected to be %v but is %v", haveCalico, tc.haveCalico)
				}
				if haveCalicoGateway != tc.haveCalicoGateway {
					log.Panicf("haveCalico is expected to be %v but is %v", haveCalicoGateway, tc.haveCalicoGateway)
				}
			})
		})
	}
}

func TestFixCalicoNetworking(t *testing.T) {
	withDummyNetworkNamespace(t, func(dummyNS ns.NetNS, dummyInfo *cnicurrent.Result) {
		withFakeCNIVeth(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
			addCalicoRoutes(t, origContVeth)
			info, err := ExtractLinkInfo(origContVeth, contNS.Path())
			if err != nil {
				log.Panicf("failed to grab interface info: %v", err)
			}
			// reuse 2nd copy of the CNI result as the dummy network config
			if err := FixCalicoNetworking(info, 24, func() (*cnicurrent.Result, string, error) {
				return dummyInfo, dummyNS.Path(), nil
			}); err != nil {
				log.Panicf("FixCalicoNetworking(): %v", err)
			}
			expectedResult := &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     innerHwAddr,
						Sandbox: contNS.Path(),
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
						Gateway: net.IP{10, 1, 90, 100},
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IP{0, 0, 0, 0},
							Mask: net.IPMask{0, 0, 0, 0},
						},
						GW: net.IP{10, 1, 90, 100},
					},
				},
			}
			if !reflect.DeepEqual(info, expectedResult) {
				t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
					spew.Sdump(expectedResult), spew.Sdump(info))
			}
		})
	})
}
