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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/nettools"
	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	netTestWaitTime = 15 * time.Second
	samplePodName   = "foobar"
	samplePodNS     = "default"
	fdKey           = "fdkey"
)

var outerAddrs = []string{
	"10.1.90.1/24",
	"10.2.90.1/24",
}

var clientAddrs = []string{
	"10.1.90.5/24",
	"10.2.90.5/24",
}

var clientMacAddrs = []string{
	"42:a4:a6:22:80:2e",
	"42:a4:a6:22:80:2f",
	"42:a4:a6:22:80:30",
}

func sampleCNIResult() *cnicurrent.Result {
	return &cnicurrent.Result{
		Interfaces: []*cnicurrent.Interface{
			{
				Name:    "eth0",
				Mac:     clientMacAddrs[0],
				Sandbox: "placeholder",
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
				Gateway: net.IP{10, 1, 90, 1},
			},
		},
		Routes: []*cnitypes.Route{
			{
				Dst: net.IPNet{
					IP:   net.IP{0, 0, 0, 0},
					Mask: net.IPMask{0, 0, 0, 0},
				},
				GW: net.IP{10, 1, 90, 1},
			},
			{
				Dst: net.IPNet{
					IP:   net.IP{10, 10, 42, 0},
					Mask: net.IPMask{255, 255, 255, 0},
				},
				GW: net.IP{10, 1, 90, 90},
			},
		},
	}
}

type vmNetworkTester struct {
	t                        *testing.T
	linkCount                int
	hostNS, contNS, clientNS ns.NetNS
	clientTapLinks           []netlink.Link
	dhcpClientTaps           []*os.File
	g                        *NetTestGroup
}

func newVMNetworkTester(t *testing.T, linkCount int) *vmNetworkTester {
	hostNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create host ns: %v", err)
	}

	clientNS, err := ns.NewNS()
	if err != nil {
		hostNS.Close()
		t.Fatalf("Failed to create ns for dhcp client: %v", err)
	}

	vnt := &vmNetworkTester{
		t:         t,
		linkCount: linkCount,
		hostNS:    hostNS,
		clientNS:  clientNS,
		g:         NewNetTestGroup(t, netTestWaitTime),
	}
	if err := vnt.setupClientTap(); err != nil {
		vnt.teardown()
		t.Fatal(err)
	}
	return vnt
}

func (vnt *vmNetworkTester) connectTaps(vmTaps []*os.File) {
	if len(vmTaps) != len(vnt.dhcpClientTaps) {
		vnt.t.Fatalf("bad number of vmTaps: %d instead of %d", len(vmTaps), len(vnt.dhcpClientTaps))
	}
	for n, vmTap := range vmTaps {
		vnt.g.Add(nil, newTapConnector(vmTap, vnt.dhcpClientTaps[n]))
	}
}

func (vnt *vmNetworkTester) addTcpdump(link netlink.Link, stopOn, failOn string) {
	tcpdump := newTcpdump(link, stopOn, failOn)
	vnt.g.Add(vnt.hostNS, tcpdump)
}

func (vnt *vmNetworkTester) verifyDhcp(iface string, expectedSubstrings []string) {
	// wait for dhcp client to complete so we don't interfere
	// with the network link too early
	<-vnt.g.Add(vnt.clientNS, NewDhcpClient(iface, expectedSubstrings))
}

func (vnt *vmNetworkTester) verifyPing(linkIndex int, outerAddr, clientAddr string) {
	// dhcpcd -T doesn't add address to the link
	outerIP := parseAddr(vnt.t, outerAddr).IP
	clientIP := parseAddr(vnt.t, clientAddr).IP
	addAddress(vnt.t, vnt.clientNS, vnt.clientTapLinks[linkIndex], clientAddr)
	vnt.g.Add(vnt.hostNS, newPinger(outerIP, clientIP))
	vnt.g.Add(vnt.clientNS, newPinger(clientIP, outerIP))
	vnt.g.Add(vnt.clientNS, newPingReceiver(clientIP))
	vnt.g.Add(vnt.hostNS, newPingReceiver(outerIP))
}

func (vnt *vmNetworkTester) wait() {
	vnt.g.Wait()
}

func (vnt *vmNetworkTester) teardown() {
	vnt.g.Stop()
	for _, tap := range vnt.dhcpClientTaps {
		// this Close() call may likely cause an error because
		// tap is probably already closed by tapConnector
		tap.Close()
	}
	for _, link := range vnt.clientTapLinks {
		if err := vnt.clientNS.Do(func(ns.NetNS) error {
			if err := netlink.LinkSetDown(link); err != nil {
				return err
			}
			if err := netlink.LinkDel(link); err != nil {
				return err
			}
			return nil
		}); err != nil {
			vnt.t.Logf("WARNING: error tearing down client tap: %v", err)
		}
	}
	vnt.clientNS.Close()
	vnt.hostNS.Close()
}

func (vnt *vmNetworkTester) setupClientTap() error {
	return vnt.clientNS.Do(func(ns.NetNS) error {
		for n := 0; n < vnt.linkCount; n++ {
			linkName := fmt.Sprintf("tap%d", n)
			clientTapLink, err := nettools.CreateTAP(linkName, 1500)
			if err != nil {
				return fmt.Errorf("CreateTAP() in the client netns: %v", err)
			}
			dhcpClientTap, err := nettools.OpenTAP(linkName)
			if err != nil {
				return fmt.Errorf("OpenTAP() in the client netns: %v", err)
			}
			mac, _ := net.ParseMAC(clientMacAddrs[n])
			if err = nettools.SetHardwareAddr(clientTapLink, mac); err != nil {
				return fmt.Errorf("can't set test MAC address on client interface: %v", err)
			}
			vnt.clientTapLinks = append(vnt.clientTapLinks, clientTapLink)
			vnt.dhcpClientTaps = append(vnt.dhcpClientTaps, dhcpClientTap)
		}
		return nil
	})
}

// TestVmNetwork verifies the network setup by directly calling
// SetupContainerSideNetwork() to rule out some possible
// TapFDSource-only errors
func TestVmNetwork(t *testing.T) {
	vnt := newVMNetworkTester(t, 1)
	defer vnt.teardown()

	contNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create container ns: %v", err)
	}
	defer contNS.Close()

	info := sampleCNIResult()

	var hostVeth, clientVeth netlink.Link
	if err := vnt.hostNS.Do(func(ns.NetNS) (err error) {
		hostVeth, clientVeth, err = nettools.CreateEscapeVethPair(contNS, "eth0", 1500)
		return
	}); err != nil {
		t.Fatalf("failed to create escape veth pair: %v", err)
	}

	clientMac, _ := net.ParseMAC(clientMacAddrs[0])

	var csn *network.ContainerSideNetwork
	if err := contNS.Do(func(ns.NetNS) error {
		netlink.LinkSetHardwareAddr(clientVeth, clientMac)
		allLinks, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("LinkList() failed: %v", err)
		}
		csn, err = nettools.SetupContainerSideNetwork(info, contNS.Path(), allLinks)
		if err != nil {
			return fmt.Errorf("failed to set up container side network: %v", err)
		}
		if len(csn.Interfaces) != 1 {
			return fmt.Errorf("single interface is expected")
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to set up container-side network: %v", err)
	}

	addAddress(t, vnt.hostNS, hostVeth, outerAddrs[0])
	vnt.connectTaps([]*os.File{csn.Interfaces[0].Fo})
	// tcpdump should catch udp 'ping' but should not
	// see BOOTP/DHCP on the 'outer' link
	vnt.addTcpdump(hostVeth, "10.1.90.1.4243 > 10.1.90.5.4242: UDP", "BOOTP/DHCP")
	vnt.g.Add(contNS, NewDhcpServerTester(csn))
	vnt.verifyDhcp("tap0", []string{
		"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
		"new_ip_address='10.1.90.5'",
		"new_network_number='10.1.90.0'",
		"new_routers='10.1.90.1'",
		"new_subnet_mask='255.255.255.0'",
		"tap0: offered 10.1.90.5 from 169.254.254.2",
	})
	vnt.verifyPing(0, outerAddrs[0], clientAddrs[0])
	vnt.wait()
}

type tapFDSourceTester struct {
	t          *testing.T
	podId      string
	cniClient  *FakeCNIClient
	tmpDir     string
	socketPath string
	s          *tapmanager.FDServer
	c          *tapmanager.FDClient
}

func newTapFDSourceTester(t *testing.T, podId string, info *cnicurrent.Result, hostNS ns.NetNS, extraRoutes map[int][]netlink.Route) *tapFDSourceTester {
	cniClient := NewFakeCNIClient()
	cniClient.ExpectPod(podId, samplePodName, samplePodNS, info, hostNS, extraRoutes)

	tmpDir, err := ioutil.TempDir("", "pass-fd-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	return &tapFDSourceTester{
		t:          t,
		podId:      podId,
		cniClient:  cniClient,
		tmpDir:     tmpDir,
		socketPath: filepath.Join(tmpDir, "tapfdserver.sock"),
	}
}

func (tst *tapFDSourceTester) stop() {
	if tst.c != nil {
		if err := tst.c.Close(); err != nil {
			tst.t.Errorf("FDClient.Close(): %v", err)
		}
		tst.c = nil
	}
	if tst.s != nil {
		if err := tst.s.Stop(); err != nil {
			tst.t.Errorf("FDServer.Stop(): %v", err)
		}
		tst.s = nil
		if err := os.Remove(tst.socketPath); err != nil {
			tst.t.Errorf("Failed to remove %q: %v", tst.socketPath, err)
		}
	}
}

func (tst *tapFDSourceTester) teardown() {
	tst.stop()
	tst.cniClient.Cleanup()
	os.RemoveAll(tst.tmpDir)
}

func (tst *tapFDSourceTester) setupServerAndConnectToFDServer() *tapmanager.FDClient {
	if tst.c != nil || tst.s != nil {
		tst.t.Fatalf("the server and/or the client is already present")
	}

	src, err := tapmanager.NewTapFDSource(tst.cniClient)
	if err != nil {
		tst.t.Fatalf("Error creating tap fd source: %v", err)
	}

	tst.s = tapmanager.NewFDServer(tst.socketPath, src)
	if err := tst.s.Serve(); err != nil {
		tst.t.Fatalf("Serve(): %v", err)
	}

	tst.c = tapmanager.NewFDClient(tst.socketPath)
	if err := tst.c.Connect(); err != nil {
		tst.t.Fatalf("Connect(): %v", err)
	}

	return tst.c
}

func verifyNoDiff(t *testing.T, what string, expected, actual interface{}) {
	expectedJson, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		expectedJson = []byte(fmt.Sprintf("<error marshalling expected: %v>", err))
	}
	actualJson, err := json.MarshalIndent(actual, "", "  ")
	if err != nil {
		actualJson = []byte(fmt.Sprintf("<error marshalling actual: %v>", err))
	}
	if bytes.Equal(expectedJson, actualJson) {
		return
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(expectedJson)),
		B:        difflib.SplitLines(string(actualJson)),
		FromFile: "Expected",
		ToFile:   "Actual",
		Context:  5,
	})
	if err != nil {
		diff = fmt.Sprintf("<diff error: %v>", err)
	}
	t.Errorf("mismatch: %s: expected:\n%s\n\nactual:\n%s\ndiff:\n%s", what, expectedJson, actualJson, diff)
}

func mustParseMAC(mac string) net.HardwareAddr {
	hwAddr, err := net.ParseMAC(mac)
	if err != nil {
		log.Panicf("Error parsing hwaddr %q: %v", mac, err)
	}
	return hwAddr
}

// TestVmNetwork verifies the network setup using TapFDSource
func TestTapFDSource(t *testing.T) {
	for _, tc := range []struct {
		// name is the name of the test case
		name string
		// interfaceCount specifies the expected interface count
		interfaceCount int
		// info specifies CNI result to return from the fake CNI
		info *cnicurrent.Result
		// dummyInfo specifies CNI result to use for the dummy Calico gateway
		dummyInfo *cnicurrent.Result
		// expectedResult specifies the expected CNI result. Defaults to the same
		// as info
		expectedResult *cnicurrent.Result
		// expectedInfoAfterTeardown specifies the expected state of the
		// network namespace after the teardown as a CNI result.
		// Defaults to the same as expectedResult
		expectedInfoAfterTeardown *cnicurrent.Result
		// tcpdumpStopOn specifies a string after which tcpdump is stopped
		tcpdumpStopOn string
		// dhcpExpectedSubstrings specifies the substrings to expect in tcpdump output
		dhcpExpectedSubstrings [][]string
		// interfaceDesc specifies the expected interface description returned by TapFDSource
		interfaceDesc []tapmanager.InterfaceDescription
		// useBadResult specifies that the result should be mangled (route and interface info removed)
		// This simulates the behavior of some CNI plugins
		useBadResult bool
		// extraRoutes map interface index from info to a slice of routes
		extraRoutes map[int][]netlink.Route
		// outerAddrs specifies per-interface addrs for the network tester peer
		outerAddrs []string
		// clientAddrs specifies per-interface VM IPs to ping
		clientAddrs []string
	}{
		{
			name:           "single cni",
			interfaceCount: 1,
			info:           sampleCNIResult(),
			tcpdumpStopOn:  "10.1.90.1.4243 > 10.1.90.5.4242: UDP",
			dhcpExpectedSubstrings: [][]string{
				{
					"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
					"new_ip_address='10.1.90.5'",
					"new_network_number='10.1.90.0'",
					"new_routers='10.1.90.1'",
					"new_subnet_mask='255.255.255.0'",
					"tap0: offered 10.1.90.5 from 169.254.254.2",
				},
			},
			interfaceDesc: []tapmanager.InterfaceDescription{
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[0]),
					FdIndex:      0,
					PCIAddress:   "",
				},
			},
			outerAddrs:  outerAddrs,
			clientAddrs: clientAddrs,
		},
		{
			name:           "multiple cnis",
			interfaceCount: 2,
			info: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[0],
						Sandbox: "placeholder",
					},
					{
						Name:    "eth1",
						Mac:     clientMacAddrs[1],
						Sandbox: "placeholder",
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
						Gateway: net.IP{10, 1, 90, 1},
					},
					{
						Version:   "4",
						Interface: 1,
						Address: net.IPNet{
							IP:   net.IP{10, 2, 90, 5},
							Mask: net.IPMask{255, 255, 255, 0},
						},
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IP{0, 0, 0, 0},
							Mask: net.IPMask{0, 0, 0, 0},
						},
						GW: net.IP{10, 1, 90, 1},
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
			tcpdumpStopOn: "10.1.90.1.4243 > 10.1.90.5.4242: UDP",
			dhcpExpectedSubstrings: [][]string{
				{
					"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
					"new_ip_address='10.1.90.5'",
					"new_network_number='10.1.90.0'",
					"new_routers='10.1.90.1'",
					"new_subnet_mask='255.255.255.0'",
					"tap0: offered 10.1.90.5 from 169.254.254.2",
				},
				{
					"new_ip_address='10.2.90.5'",
					"new_network_number='10.2.90.0'",
					"new_subnet_mask='255.255.255.0'",
					"tap1: offered 10.2.90.5 from 169.254.254.2",
				},
			},
			interfaceDesc: []tapmanager.InterfaceDescription{
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[0]),
					FdIndex:      0,
					PCIAddress:   "",
				},
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[1]),
					FdIndex:      1,
					PCIAddress:   "",
				},
			},
			outerAddrs:  outerAddrs,
			clientAddrs: clientAddrs,
		},
		{
			name:           "multiple cnis (reordered interfaces)",
			interfaceCount: 2,
			info: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth1",
						Mac:     clientMacAddrs[1],
						Sandbox: "placeholder",
					},
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[0],
						Sandbox: "placeholder",
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version:   "4",
						Interface: 1,
						Address: net.IPNet{
							IP:   net.IP{10, 1, 90, 5},
							Mask: net.IPMask{255, 255, 255, 0},
						},
						Gateway: net.IP{10, 1, 90, 1},
					},
					{
						Version:   "4",
						Interface: 0,
						Address: net.IPNet{
							IP:   net.IP{10, 2, 90, 5},
							Mask: net.IPMask{255, 255, 255, 0},
						},
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IP{0, 0, 0, 0},
							Mask: net.IPMask{0, 0, 0, 0},
						},
						GW: net.IP{10, 1, 90, 1},
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
			expectedInfoAfterTeardown: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[0],
						Sandbox: "placeholder",
					},
					{
						Name:    "eth1",
						Mac:     clientMacAddrs[1],
						Sandbox: "placeholder",
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
						Gateway: net.IP{10, 1, 90, 1},
					},
					{
						Version:   "4",
						Interface: 1,
						Address: net.IPNet{
							IP:   net.IP{10, 2, 90, 5},
							Mask: net.IPMask{255, 255, 255, 0},
						},
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IP{0, 0, 0, 0},
							Mask: net.IPMask{0, 0, 0, 0},
						},
						GW: net.IP{10, 1, 90, 1},
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
			tcpdumpStopOn: "10.1.90.1.4243 > 10.1.90.5.4242: UDP",
			dhcpExpectedSubstrings: [][]string{
				{
					"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
					"new_ip_address='10.1.90.5'",
					"new_network_number='10.1.90.0'",
					"new_routers='10.1.90.1'",
					"new_subnet_mask='255.255.255.0'",
					"tap0: offered 10.1.90.5 from 169.254.254.2",
				},
				{
					"new_ip_address='10.2.90.5'",
					"new_network_number='10.2.90.0'",
					"new_subnet_mask='255.255.255.0'",
					"tap1: offered 10.2.90.5 from 169.254.254.2",
				},
			},
			interfaceDesc: []tapmanager.InterfaceDescription{
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[0]),
					FdIndex:      0,
					PCIAddress:   "",
				},
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[1]),
					FdIndex:      1,
					PCIAddress:   "",
				},
			},
			outerAddrs:  outerAddrs,
			clientAddrs: clientAddrs,
		},
		{
			name:           "single cni with bad result correction",
			interfaceCount: 1,
			info:           sampleCNIResult(),
			tcpdumpStopOn:  "10.1.90.1.4243 > 10.1.90.5.4242: UDP",
			dhcpExpectedSubstrings: [][]string{
				{
					"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
					"new_ip_address='10.1.90.5'",
					"new_network_number='10.1.90.0'",
					"new_routers='10.1.90.1'",
					"new_subnet_mask='255.255.255.0'",
					"tap0: offered 10.1.90.5 from 169.254.254.2",
				},
			},
			interfaceDesc: []tapmanager.InterfaceDescription{
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[0]),
					FdIndex:      0,
					PCIAddress:   "",
				},
			},
			useBadResult: true,
			outerAddrs:   outerAddrs,
			clientAddrs:  clientAddrs,
		},
		{
			name:           "calico",
			interfaceCount: 1,
			info: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[0],
						Sandbox: "placeholder",
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version:   "4",
						Interface: 0,
						Address: net.IPNet{
							IP:   net.IP{192, 168, 135, 131},
							Mask: net.IPMask{255, 255, 255, 255},
						},
					},
				},
			},
			dummyInfo: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[2],
						Sandbox: "placeholder",
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version:   "4",
						Interface: 0,
						Address: net.IPNet{
							IP:   net.IP{192, 168, 135, 132},
							Mask: net.IPMask{255, 255, 255, 255},
						},
					},
				},
			},
			expectedResult: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[0],
						Sandbox: "placeholder",
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version:   "4",
						Interface: 0,
						Address: net.IPNet{
							IP:   net.IP{192, 168, 135, 131},
							Mask: net.IPMask{255, 255, 255, 0},
						},
						Gateway: net.IP{192, 168, 135, 132},
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IP{0, 0, 0, 0},
							Mask: net.IPMask{0, 0, 0, 0},
						},
						GW: net.IP{192, 168, 135, 132},
					},
				},
			},
			extraRoutes: map[int][]netlink.Route{
				0: {
					{
						Dst:   parseAddr(t, "169.254.1.1/32").IPNet,
						Scope: nettools.SCOPE_LINK,
					},
					{
						Gw:    parseAddr(t, "169.254.1.1/32").IPNet.IP,
						Scope: nettools.SCOPE_UNIVERSE,
					},
				},
			},
			tcpdumpStopOn: "192.168.135.100.4243 > 192.168.135.131.4242: UDP",
			dhcpExpectedSubstrings: [][]string{
				{
					"new_ip_address='192.168.135.131'",
					"new_network_number='192.168.135.0'",
					"new_routers='192.168.135.132'",
					"new_subnet_mask='255.255.255.0'",
					"tap0: offered 192.168.135.131 from 169.254.254.2",
				},
			},
			interfaceDesc: []tapmanager.InterfaceDescription{
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[0]),
					FdIndex:      0,
					PCIAddress:   "",
				},
			},
			outerAddrs:   []string{"192.168.135.100/24"},
			clientAddrs:  []string{"192.168.135.131/24"},
			useBadResult: true,
		},
		{
			name:           "calico with multiple cnis",
			interfaceCount: 2,
			info: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[0],
						Sandbox: "placeholder",
					},
					{
						Name:    "eth1",
						Mac:     clientMacAddrs[1],
						Sandbox: "placeholder",
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version:   "4",
						Interface: 0,
						Address: net.IPNet{
							IP:   net.IP{192, 168, 135, 131},
							Mask: net.IPMask{255, 255, 255, 255},
						},
					},
					{
						Version:   "4",
						Interface: 1,
						Address: net.IPNet{
							IP:   net.IP{10, 2, 90, 5},
							Mask: net.IPMask{255, 255, 255, 0},
						},
					},
				},
			},
			dummyInfo: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[2],
						Sandbox: "placeholder",
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version:   "4",
						Interface: 0,
						Address: net.IPNet{
							IP:   net.IP{192, 168, 135, 132},
							Mask: net.IPMask{255, 255, 255, 255},
						},
					},
				},
			},
			expectedResult: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddrs[0],
						Sandbox: "placeholder",
					},
					{
						Name:    "eth1",
						Mac:     clientMacAddrs[1],
						Sandbox: "placeholder",
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version:   "4",
						Interface: 0,
						Address: net.IPNet{
							IP:   net.IP{192, 168, 135, 131},
							Mask: net.IPMask{255, 255, 255, 0},
						},
						Gateway: net.IP{192, 168, 135, 132},
					},
					{
						Version:   "4",
						Interface: 1,
						Address: net.IPNet{
							IP:   net.IP{10, 2, 90, 5},
							Mask: net.IPMask{255, 255, 255, 0},
						},
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IP{0, 0, 0, 0},
							Mask: net.IPMask{0, 0, 0, 0},
						},
						GW: net.IP{192, 168, 135, 132},
					},
				},
			},
			extraRoutes: map[int][]netlink.Route{
				0: {
					{
						Dst:   parseAddr(t, "169.254.1.1/32").IPNet,
						Scope: nettools.SCOPE_LINK,
					},
					{
						Gw:    parseAddr(t, "169.254.1.1/32").IPNet.IP,
						Scope: nettools.SCOPE_UNIVERSE,
					},
				},
			},
			tcpdumpStopOn: "192.168.135.100.4243 > 192.168.135.131.4242: UDP",
			dhcpExpectedSubstrings: [][]string{
				{
					"new_ip_address='192.168.135.131'",
					"new_network_number='192.168.135.0'",
					"new_routers='192.168.135.132'",
					"new_subnet_mask='255.255.255.0'",
					"tap0: offered 192.168.135.131 from 169.254.254.2",
				},
				{
					"new_ip_address='10.2.90.5'",
					"new_network_number='10.2.90.0'",
					"new_subnet_mask='255.255.255.0'",
					"tap1: offered 10.2.90.5 from 169.254.254.2",
				},
			},
			interfaceDesc: []tapmanager.InterfaceDescription{
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[0]),
					FdIndex:      0,
					PCIAddress:   "",
				},
				{
					Type:         network.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddrs[1]),
					FdIndex:      1,
					PCIAddress:   "",
				},
			},
			outerAddrs: []string{
				"192.168.135.100/24",
				"10.2.90.1/24",
			},
			clientAddrs: []string{
				"192.168.135.131/24",
				"10.2.90.5/24",
			},
			useBadResult: true,
		},
	} {
		for _, recover := range []bool{false, true} {
			name := tc.name
			if recover {
				name += "/recover"
			}
			t.Run(name, func(t *testing.T) {
				podId := utils.NewUUID()
				vnt := newVMNetworkTester(t, tc.interfaceCount)
				defer vnt.teardown()

				tst := newTapFDSourceTester(t, podId, tc.info, vnt.hostNS, tc.extraRoutes)
				defer tst.teardown()
				c := tst.setupServerAndConnectToFDServer()
				if tc.dummyInfo != nil {
					tst.cniClient.ExpectDummyPod(tc.dummyInfo, vnt.hostNS, tc.extraRoutes)
				}
				tst.cniClient.UseBadResult(tst.podId, samplePodName, samplePodNS, tc.useBadResult)
				csnBytes, err := c.AddFDs(fdKey, &tapmanager.GetFDPayload{
					Description: &tapmanager.PodNetworkDesc{
						PodID:   tst.podId,
						PodNs:   samplePodNS,
						PodName: samplePodName,
					},
				})
				if err != nil {
					t.Fatalf("AddFDs(): %v", err)
				}
				released := false
				defer func() {
					if !released {
						c.ReleaseFDs(fdKey)
					}
				}()

				var expectedResult *cnicurrent.Result
				var csn *network.ContainerSideNetwork
				if err := json.Unmarshal(csnBytes, &csn); err != nil {
					t.Errorf("error unmarshalling containser side network: %v", err)
				} else {
					if tc.expectedResult != nil {
						expectedResult = copyCNIResult(tc.expectedResult)
					} else {
						expectedResult = copyCNIResult(tc.info)
					}
					replaceSandboxPlaceholders(expectedResult, tst.podId)
					verifyNoDiff(t, "cni result", expectedResult, csn.Result)
				}
				var expectedInfoAfterTeardown *cnicurrent.Result
				if tc.expectedInfoAfterTeardown != nil {
					expectedInfoAfterTeardown = copyCNIResult(tc.expectedInfoAfterTeardown)
					replaceSandboxPlaceholders(expectedInfoAfterTeardown, tst.podId)
				} else {
					expectedInfoAfterTeardown = expectedResult
				}

				tst.cniClient.VerifyAdded(tst.podId, samplePodName, samplePodNS)
				veths := tst.cniClient.Veths(tst.podId, samplePodName, samplePodNS)
				if len(veths) != tc.interfaceCount {
					t.Fatalf("veth count mismatch: %d instead of %d", len(veths), tc.interfaceCount)
				}

				fds, descBytes, err := c.GetFDs(fdKey)
				if err != nil {
					t.Fatalf("GetFDs(): %v", err)
				}
				if len(fds) != tc.interfaceCount {
					t.Fatalf("fd count mismatch: %d instead of %d", len(fds), tc.interfaceCount)
				}

				vmTaps := []*os.File{}

				if recover {
					// Duplicate FDs to make them survive TapFDSource.Stop()
					// which closes the tap devices. The FDs will be closed
					// by tapConnector
					for n, fd := range fds {
						fds[n], err = syscall.Dup(fd)
						if err != nil {
							t.Fatalf("Dup(): %v", err)
						}
					}

					tst.stop()
					c = tst.setupServerAndConnectToFDServer()
					if err = c.Recover(fdKey, &tapmanager.GetFDPayload{
						ContainerSideNetwork: csn,
						Description: &tapmanager.PodNetworkDesc{
							PodID:   tst.podId,
							PodNs:   samplePodNS,
							PodName: samplePodName,
						},
					}); err != nil {
						t.Fatalf("Recover(): %v", err)
					}
				}

				for _, fd := range fds {
					vmTap := os.NewFile(uintptr(fd), "tap-fd")
					vmTaps = append(vmTaps, vmTap)
				}

				var interfaceDesc []tapmanager.InterfaceDescription
				if err := json.Unmarshal(descBytes, &interfaceDesc); err != nil {
					t.Errorf("error unmarshalling interface desc: %v", err)
				} else {
					verifyNoDiff(t, "interfaceDesc", tc.interfaceDesc, interfaceDesc)
				}

				for n, veth := range veths {
					addAddress(t, vnt.hostNS, veth.HostSide, tc.outerAddrs[n])
				}

				vnt.connectTaps(vmTaps)
				// tcpdump should catch udp 'ping' but should not
				// see BOOTP/DHCP on the 'outer' link
				vnt.addTcpdump(veths[0].HostSide, tc.tcpdumpStopOn, "BOOTP/DHCP")
				for n, substrings := range tc.dhcpExpectedSubstrings {
					vnt.verifyDhcp(fmt.Sprintf("tap%d", n), substrings)
				}
				for n := range veths {
					vnt.verifyPing(n, tc.outerAddrs[n], tc.clientAddrs[n])
				}
				vnt.wait()

				if err := c.ReleaseFDs(fdKey); err != nil {
					t.Errorf("ReleaseFDs(): %v", err)
				}
				released = true
				tst.cniClient.VerifyRemoved(tst.podId, samplePodName, samplePodNS)

				infoAfterTeardown := tst.cniClient.NetworkInfoAfterTeardown(tst.podId, samplePodName, samplePodNS)
				verifyNoDiff(t, "network info after teardown", expectedInfoAfterTeardown, infoAfterTeardown)
			})
		}
	}
}

// TODO: test DNS handling
// TODO: test SR-IOV (by making a fake sysfs dir)
