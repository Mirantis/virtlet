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
	"encoding/json"
	"log"
	"net"
	"reflect"
	"testing"

	"github.com/containernetworking/cni/pkg/ns"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/vishvananda/netlink"
)

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

func verifyCalicoNetworking(t *testing.T, info *cnicurrent.Result, contNSPath string) {
	// add dummy ipconfig to make it appear for FixCalicoNetworking
	// as multi-CNI result
	info.IPs = append(info.IPs, &cnicurrent.IPConfig{})

	if err := FixCalicoNetworking(info); err != nil {
		log.Panicf("FixCalicoNetworking(): %v", err)
	}

	expectedResult := expectedExtractedCalicoLinkInfo(contNSPath)
	expectedResult.IPs = append(expectedResult.IPs, &cnicurrent.IPConfig{})
	if !reflect.DeepEqual(info, expectedResult) {
		t.Errorf("interface info mismatch. Expected:\n%s\nActual:\n%s",
			spew.Sdump(expectedResult), spew.Sdump(info))
	}

	serializedInfo, err := json.Marshal(info)
	if err != nil {
		log.Panicf("marshalling CNI result: %v", err)
	}

	if err := FixCalicoNetworking(info); err != nil {
		log.Panicf("[repeated] FixCalicoNetworking(): %v", err)
	}

	newSerializedInfo, err := json.Marshal(info)
	if err != nil {
		log.Panicf("marshalling CNI result: %v", err)
	}

	if string(serializedInfo) != string(newSerializedInfo) {
		t.Errorf("FixCalicoNetworking not idempotent: was: %s now: %s", serializedInfo, newSerializedInfo)
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
			verifyCalicoNetworking(t, info, contNS.Path())
		})
	})
}

func TestFixCalicoNetworkingWithTruncatedResult(t *testing.T) {
	// this covers multiple CNI case when ExtractLinkInfo is not available
	withDummyNetworkNamespace(t, func(dummyNS ns.NetNS, dummyInfo *cnicurrent.Result) {
		withFakeCNIVeth(t, defaultMTU, func(hostNS, contNS ns.NetNS, origHostVeth, origContVeth netlink.Link) {
			addCalicoRoutes(t, origContVeth)
			info := &cnicurrent.Result{
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
							IP: net.IP{10, 1, 90, 5},
							// FIXME: actually, Calico uses
							// 255.255.255.255 as netmask here
							// (but we don't want to overcomplicate the test)
							Mask: net.IPMask{255, 255, 255, 0},
						},
					},
				},
			}
			verifyCalicoNetworking(t, info, contNS.Path())
		})
	})
}
