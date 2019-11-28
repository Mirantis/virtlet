/*
Copyright 2019 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Parts of this file are copied from https://github.com/google/netboot/blob/8e5c0d07937f8c1dea6e5f218b64f6b95c32ada3/pixiecore/dhcp.go

*/

package dhcp

import (
	"bytes"
	"net"
	"testing"

	"github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"

	"github.com/Mirantis/virtlet/pkg/network"
)

func TestGetStaticRoutes(t *testing.T) {
	for _, tc := range []struct {
		name           string
		ip             net.IPNet
		cniResult      *cnicurrent.Result
		expectedRouter []byte
		expectedRoutes []byte
	}{
		{
			name: "simple routes",
			ip: net.IPNet{
				IP:   net.IPv4(192, 168, 7, 10),
				Mask: net.CIDRMask(24, 32),
			},
			cniResult: &cnicurrent.Result{
				IPs: []*cnicurrent.IPConfig{
					{
						Version: "4",
						Address: net.IPNet{
							IP:   net.IPv4(192, 168, 7, 10),
							Mask: net.CIDRMask(24, 32),
						},
					},
				},
				Routes: []*types.Route{
					{
						Dst: net.IPNet{
							IP:   net.IPv4(10, 0, 0, 0),
							Mask: net.CIDRMask(8, 32),
						},
						GW: net.IPv4(192, 168, 7, 5),
					},
					{
						Dst: net.IPNet{
							IP:   net.IPv4(0, 0, 0, 0),
							Mask: net.CIDRMask(0, 32),
						},
						GW: net.IPv4(192, 168, 7, 1),
					},
				},
			},
			expectedRouter: nil,
			expectedRoutes: []byte{8, 10, 192, 168, 7, 5, 0, 192, 168, 7, 1},
		},
		{
			name: "router only",
			ip: net.IPNet{
				IP:   net.IPv4(192, 168, 7, 10),
				Mask: net.CIDRMask(24, 32),
			},
			cniResult: &cnicurrent.Result{
				IPs: []*cnicurrent.IPConfig{
					{
						Version: "4",
						Address: net.IPNet{
							IP:   net.IPv4(192, 168, 7, 10),
							Mask: net.CIDRMask(24, 32),
						},
					},
				},
				Routes: []*types.Route{
					{
						Dst: net.IPNet{
							IP:   net.IPv4(0, 0, 0, 0),
							Mask: net.CIDRMask(0, 32),
						},
						GW: net.IPv4(192, 168, 7, 1),
					},
				},
			},
			expectedRouter: []byte{192, 168, 7, 1},
			expectedRoutes: nil,
		},
		{
			name: "calico",
			ip: net.IPNet{
				IP:   net.IPv4(192, 168, 7, 10),
				Mask: net.CIDRMask(24, 32),
			},
			cniResult: &cnicurrent.Result{
				IPs: []*cnicurrent.IPConfig{
					{
						Version: "4",
						Address: net.IPNet{
							IP:   net.IPv4(192, 168, 7, 10),
							Mask: net.CIDRMask(32, 32),
						},
						Gateway: net.IPv4(169, 254, 1, 1),
					},
				},
				Routes: []*types.Route{
					{
						Dst: net.IPNet{
							IP:   net.IPv4(169, 254, 1, 1),
							Mask: net.CIDRMask(32, 32),
						},
						GW: net.IP{0, 0, 0, 0},
					},
					{
						Dst: net.IPNet{
							IP:   net.IPv4(0, 0, 0, 0),
							Mask: net.CIDRMask(0, 32),
						},
						GW: net.IPv4(169, 254, 1, 1),
					},
				},
			},
			expectedRouter: nil,
			expectedRoutes: []byte{32, 169, 254, 1, 1, 0, 0, 0, 0, 0, 169, 254, 1, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				config: &network.ContainerSideNetwork{
					Result: tc.cniResult,
				},
			}

			router, routes, err := s.getStaticRoutes(tc.ip)
			if err != nil {
				t.Fatalf("getStaticRoutes(): %v", err)
			}

			if !bytes.Equal(tc.expectedRouter, router) {
				t.Errorf("bad router: expected %v, got %v", tc.expectedRouter, router)
			}

			if !bytes.Equal(tc.expectedRoutes, routes) {
				t.Errorf("bad routes: expected %v, got %v", tc.expectedRoutes, routes)
			}
		})
	}
}
