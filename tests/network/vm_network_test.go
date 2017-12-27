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
	"testing"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/nettools"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	sampleOuterAddr  = "10.1.90.1/24"
	clientAddr       = "10.1.90.5/24"
	clientMacAddress = "42:a4:a6:22:80:2e"
	netTestWaitTime  = 15 * time.Second
	samplePodName    = "foobar"
	samplePodNS      = "default"
	fdKey            = "fdkey"
)

type vmNetworkTester struct {
	t                        *testing.T
	hostNS, contNS, clientNS ns.NetNS
	dhcpClientTap            *os.File
	clientTapLink            netlink.Link
	g                        *NetTestGroup
}

func newVMNetworkTester(t *testing.T) *vmNetworkTester {
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
		t:        t,
		hostNS:   hostNS,
		clientNS: clientNS,
		g:        NewNetTestGroup(t, netTestWaitTime),
	}
	if err := vnt.setupClientTap(); err != nil {
		vnt.teardown()
		t.Fatal(err)
	}
	return vnt
}

func (vnt *vmNetworkTester) connectTaps(vmTap *os.File) {
	vnt.g.Add(nil, newTapConnector(vmTap, vnt.dhcpClientTap))
}

func (vnt *vmNetworkTester) addTcpdump(link netlink.Link, stopOn, failOn string) {
	tcpdump := newTcpdump(link, stopOn, failOn)
	vnt.g.Add(vnt.hostNS, tcpdump)
}

func (vnt *vmNetworkTester) verifyDhcp(expectedSubstrings []string) {
	// wait for dhcp client to complete so we don't interfere
	// with the network link too early
	<-vnt.g.Add(vnt.clientNS, NewDhcpClient(expectedSubstrings))
}

func (vnt *vmNetworkTester) verifyPing(outerIP net.IP) {
	// dhcpcd -T doesn't add address to the link
	clientIP := addAddress(vnt.t, vnt.clientNS, vnt.clientTapLink, clientAddr)
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
	if vnt.dhcpClientTap != nil {
		// this Close() call may likely cause an error because
		// tap is probably already closed by tapConnector
		vnt.dhcpClientTap.Close()
	}
	if vnt.clientTapLink != nil {
		if err := vnt.clientNS.Do(func(ns.NetNS) error {
			if err := netlink.LinkSetDown(vnt.clientTapLink); err != nil {
				return err
			}
			if err := netlink.LinkDel(vnt.clientTapLink); err != nil {
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
		var err error
		vnt.clientTapLink, err = nettools.CreateTAP("tap0", 1500)
		if err != nil {
			return fmt.Errorf("CreateTAP() in the client netns: %v", err)
		}
		vnt.dhcpClientTap, err = nettools.OpenTAP("tap0")
		if err != nil {
			return fmt.Errorf("OpenTAP() in the client netns: %v", err)
		}
		mac, _ := net.ParseMAC(clientMacAddress)
		if err = nettools.SetHardwareAddr(vnt.clientTapLink, mac); err != nil {
			return fmt.Errorf("can't set test MAC address on client interface: %v", err)
		}
		return nil
	})
}

// TestVmNetwork verifies the network setup by directly calling
// SetupContainerSideNetwork() to rule out some possible
// TapFDSource-only errors
func TestVmNetwork(t *testing.T) {
	vnt := newVMNetworkTester(t)
	defer vnt.teardown()

	contNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create container ns: %v", err)
	}
	defer contNS.Close()

	info := &cnicurrent.Result{
		Interfaces: []*cnicurrent.Interface{
			{
				Name:    "eth0",
				Mac:     clientMacAddress,
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

	var hostVeth netlink.Link
	if err := vnt.hostNS.Do(func(ns.NetNS) (err error) {
		hostVeth, _, err = nettools.CreateEscapeVethPair(contNS, "eth0", 1500)
		return
	}); err != nil {
		t.Fatalf("failed to create escape veth pair: %v", err)
	}

	var csn *nettools.ContainerSideNetwork
	if err := contNS.Do(func(ns.NetNS) error {
		allLinks, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("LinkList() failed: %v", err)
		}
		csn, err = nettools.SetupContainerSideNetwork(info, contNS.Path(), allLinks)
		if err != nil {
			return fmt.Errorf("failed to set up container side network: %v", err)
		}
		if len(csn.Fds) != 1 {
			return fmt.Errorf("single tap fd is expected")
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to set up container-side network: %v", err)
	}

	outerIP := addAddress(t, vnt.hostNS, hostVeth, sampleOuterAddr)
	vnt.connectTaps(csn.Fds[0])
	// tcpdump should catch udp 'ping' but should not
	// see BOOTP/DHCP on the 'outer' link
	vnt.addTcpdump(hostVeth, "10.1.90.1.4243 > 10.1.90.5.4242: UDP", "BOOTP/DHCP")
	vnt.g.Add(contNS, NewDhcpServerTester(info))
	vnt.verifyDhcp([]string{
		"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
		"new_ip_address='10.1.90.5'",
		"new_network_number='10.1.90.0'",
		"new_routers='10.1.90.1'",
		"new_subnet_mask='255.255.255.0'",
		"tap0: offered 10.1.90.5 from 169.254.254.2",
	})
	vnt.verifyPing(outerIP)
	vnt.wait()
}

func withTapFDSource(t *testing.T, info *cnicurrent.Result, hostNS ns.NetNS, toCall func(string, *FakeCNIClient, *tapmanager.FDClient)) {
	podId := utils.NewUuid()
	cniClient := NewFakeCNIClient(info, hostNS, podId, samplePodName, samplePodNS)
	defer cniClient.Cleanup()

	src, err := tapmanager.NewTapFDSource(cniClient)
	if err != nil {
		t.Fatalf("Error creating tap fd source: %v", err)
	}

	tmpDir, err := ioutil.TempDir("", "pass-fd-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)
	socketPath := filepath.Join(tmpDir, "tapfdserver.sock")

	s := tapmanager.NewFDServer(socketPath, src)
	if err := s.Serve(); err != nil {
		t.Fatalf("Serve(): %v", err)
	}
	defer s.Stop()

	c := tapmanager.NewFDClient(socketPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect(): %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	}()
	toCall(podId, cniClient, c)
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
		name                   string
		interfaceCount         int
		outerAddr              string
		info                   *cnicurrent.Result
		tcpdumpStopOn          string
		dhcpExpectedSubstrings []string
		interfaceDesc          []tapmanager.InterfaceDescription
	}{
		{
			name:           "single cni",
			interfaceCount: 1,
			outerAddr:      sampleOuterAddr,
			info: &cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "eth0",
						Mac:     clientMacAddress,
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
			},
			tcpdumpStopOn: "10.1.90.1.4243 > 10.1.90.5.4242: UDP",
			dhcpExpectedSubstrings: []string{
				"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
				"new_ip_address='10.1.90.5'",
				"new_network_number='10.1.90.0'",
				"new_routers='10.1.90.1'",
				"new_subnet_mask='255.255.255.0'",
				"tap0: offered 10.1.90.5 from 169.254.254.2",
			},
			interfaceDesc: []tapmanager.InterfaceDescription{
				{
					Type:         nettools.InterfaceTypeTap,
					HardwareAddr: mustParseMAC(clientMacAddress),
					FdIndex:      0,
					PCIAddress:   "",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vnt := newVMNetworkTester(t)
			defer vnt.teardown()

			withTapFDSource(t, tc.info, vnt.hostNS, func(podId string, cniClient *FakeCNIClient, c *tapmanager.FDClient) {
				netConfigBytes, err := c.AddFDs(fdKey, &tapmanager.GetFDPayload{
					Description: &tapmanager.PodNetworkDesc{
						PodId:   podId,
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

				var result, expectedResult *cnicurrent.Result
				if err := json.Unmarshal(netConfigBytes, &result); err != nil {
					t.Errorf("error unmarshalling CNI result: %v", err)
				} else {
					expectedResult = copyCNIResult(tc.info)
					replaceSandboxPlaceholders(expectedResult, podId)
					verifyNoDiff(t, "cni result", expectedResult, result)
				}

				cniClient.VerifyAdded()
				veths := cniClient.Veths()
				if len(veths) != 1 {
					t.Fatalf("veth count mismatch: %d instead of %d", len(veths), tc.interfaceCount)
				}

				fds, descBytes, err := c.GetFDs(fdKey)
				if err != nil {
					t.Fatalf("GetFDs(): %v", err)
				}
				if len(fds) != tc.interfaceCount {
					t.Fatalf("fd count mismatch: %d instead of %d", len(fds), tc.interfaceCount)
				}

				var interfaceDesc []tapmanager.InterfaceDescription
				if err := json.Unmarshal(descBytes, &interfaceDesc); err != nil {
					t.Errorf("error unmarshalling interface desc: %v", err)
				} else {
					verifyNoDiff(t, "interfaceDesc", tc.interfaceDesc, interfaceDesc)
				}

				vmTap := os.NewFile(uintptr(fds[0]), "tap-fd")
				defer vmTap.Close()
				outerIP := addAddress(t, vnt.hostNS, veths[0].HostSide, tc.outerAddr)
				vnt.connectTaps(vmTap)
				// tcpdump should catch udp 'ping' but should not
				// see BOOTP/DHCP on the 'outer' link
				vnt.addTcpdump(veths[0].HostSide, tc.tcpdumpStopOn, "BOOTP/DHCP")
				vnt.verifyDhcp(tc.dhcpExpectedSubstrings)
				vnt.verifyPing(outerIP)
				vnt.wait()

				if err := c.ReleaseFDs(fdKey); err != nil {
					t.Errorf("ReleaseFDs(): %v", err)
				}
				released = true
				cniClient.VerifyRemoved()

				infoAfterTeardown := cniClient.NetworkInfoAfterTeardown()
				verifyNoDiff(t, "network info after teardown", expectedResult, infoAfterTeardown)
			})
		})
	}
}

// TODO: test multiple CNIs
// TODO: test Calico
// TODO: test recovering netns
// TODO: test SR-IOV
