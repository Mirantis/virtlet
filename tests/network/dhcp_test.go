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

package network

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/dhcp"
	"github.com/Mirantis/virtlet/pkg/nettools"
)

type dhcpTestCase struct {
	info               types.Result
	expectedSubstrings []string
}

func TestDhcpServer(t *testing.T) {
	testCases := []*dhcpTestCase{
		{
			info: types.Result{
				IP4: &types.IPConfig{
					IP: net.IPNet{
						IP:   net.IP{10, 1, 90, 5},
						Mask: net.IPMask{255, 255, 255, 0},
					},
					Gateway: net.IP{10, 1, 90, 1},
					Routes: []types.Route{
						{
							Dst: net.IPNet{
								IP:   net.IP{0, 0, 0, 0},
								Mask: net.IPMask{0, 0, 0, 0},
							},
						},
						{
							Dst: net.IPNet{
								IP:   net.IP{10, 10, 42, 0},
								Mask: net.IPMask{255, 255, 255, 0},
							},
							GW: net.IP{10, 1, 90, 90},
						},
					},
				},
			},
			expectedSubstrings: []string{
				"new_broadcast_address='10.1.90.255'",
				"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
				"new_dhcp_lease_time='86400'",
				"new_dhcp_rebinding_time='64800'",
				"new_dhcp_renewal_time='43200'",
				"new_dhcp_server_identifier='169.254.254.2'",
				"new_domain_name_servers='8.8.8.8'",
				"new_ip_address='10.1.90.5'",
				"new_network_number='10.1.90.0'",
				"new_routers='10.1.90.1'",
				"new_subnet_cidr='24'",
				"new_subnet_mask='255.255.255.0'",
				"veth0: offered 10.1.90.5 from 169.254.254.2",
			},
		},
		{
			info: types.Result{
				IP4: &types.IPConfig{
					IP: net.IPNet{
						IP:   net.IP{10, 1, 90, 5},
						Mask: net.IPMask{255, 255, 255, 0},
					},
					Routes: []types.Route{
						{
							Dst: net.IPNet{
								IP:   net.IP{0, 0, 0, 0},
								Mask: net.IPMask{0, 0, 0, 0},
							},
							GW: net.IP{169, 254, 1, 1},
						},
						{
							Dst: net.IPNet{
								IP:   net.IP{169, 254, 1, 1},
								Mask: net.IPMask{255, 255, 255, 255},
							},
						},
					},
				},
				DNS: types.DNS{
					Nameservers: []string{"10.1.90.99"},
					Search:      []string{"test.address"},
				},
			},
			expectedSubstrings: []string{
				"new_broadcast_address='10.1.90.255'",
				"new_classless_static_routes='169.254.1.1/32 0.0.0.0'",
				"new_dhcp_lease_time='86400'",
				"new_dhcp_rebinding_time='64800'",
				"new_dhcp_renewal_time='43200'",
				"new_dhcp_server_identifier='169.254.254.2'",
				"new_domain_name_servers='10.1.90.99'",
				"new_domain_search='test.address'",
				"new_ip_address='10.1.90.5'",
				"new_network_number='10.1.90.0'",
				"new_routers='169.254.1.1'",
				"new_subnet_cidr='24'",
				"new_subnet_mask='255.255.255.0'",
				"veth0: offered 10.1.90.5 from 169.254.254.2",
			},
		},
	}

	for _, testCase := range testCases {
		// TODO: use subtests https://golang.org/pkg/testing/#hdr-Subtests_and_Sub_benchmarks
		// (need newer Go)
		runDhcpTestCase(t, testCase)
	}
}

func runDhcpTestCase(t *testing.T, testCase *dhcpTestCase) {
	serverNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create ns for dhcp server: %v", err)
	}
	defer serverNS.Close()
	clientNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create ns for dhcp client: %v", err)
	}
	defer clientNS.Close()
	var clientVeth, serverVeth netlink.Link
	if err := serverNS.Do(func(ns.NetNS) error {
		serverVeth, clientVeth, err = nettools.CreateEscapeVethPair(clientNS, "veth0", 1500)
		if err != nil {
			return fmt.Errorf("failed to create escape veth pair: %v", err)
		}
		addr, err := netlink.ParseAddr("169.254.254.2/24")
		if err != nil {
			return fmt.Errorf("failed to parse dhcp interface address", err)
		}
		if err = netlink.AddrAdd(serverVeth, addr); err != nil {
			return fmt.Errorf("failed to add addr for serverVeth: %v", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	g := NewNetTestGroup(t, 1*time.Minute)
	defer g.Stop()
	g.Add(serverNS, NewDhcpServerTester(&dhcp.Config{
		CNIResult:           testCase.info,
		PeerHardwareAddress: clientVeth.Attrs().HardwareAddr,
	}))

	g.Add(clientNS, NewDhcpClient(testCase.expectedSubstrings))
	g.Wait()
}
