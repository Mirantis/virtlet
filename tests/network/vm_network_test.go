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
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/glog"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/nettools"
)

const (
	pingPort                  = 4242
	pingSrcPort               = 4243
	pingInterval              = 100 * time.Millisecond
	pingDeadline              = 55 * time.Millisecond
	pingReceiverCycles        = 100
	tcpdumpPollPeriod         = 50 * time.Millisecond
	tcpdumpStartupPollCount   = 100
	tcpdumpSubstringWaitCount = 100
	outerAddr                 = "10.1.90.1/24"
	clientAddr                = "10.1.90.5/24"
	clientMacAddress          = "42:a4:a6:22:80:2e"
	netTestWaitTime           = 15 * time.Second
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

	contNS, err := ns.NewNS()
	if err != nil {
		hostNS.Close()
		t.Fatalf("Failed to create container ns: %v", err)
	}

	clientNS, err := ns.NewNS()
	if err != nil {
		hostNS.Close()
		contNS.Close()
		t.Fatalf("Failed to create ns for dhcp client: %v", err)
	}

	vnt := &vmNetworkTester{
		t:        t,
		hostNS:   hostNS,
		contNS:   contNS,
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

func (vnt *vmNetworkTester) verifyDhcp(info *cnicurrent.Result, expectedSubstrings ...string) {
	vnt.g.Add(vnt.contNS, NewDhcpServerTester(info))
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
	vnt.contNS.Close()
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

	info := &cnicurrent.Result{
		Interfaces: []*cnicurrent.Interface{
			{
				Name:    "eth0",
				Mac:     clientMacAddress,
				Sandbox: vnt.contNS.Path(),
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
		hostVeth, _, err = nettools.CreateEscapeVethPair(vnt.contNS, "eth0", 1500)
		return
	}); err != nil {
		t.Fatalf("failed to create escape veth pair: %v", err)
	}

	var csn *nettools.ContainerSideNetwork
	if err := vnt.contNS.Do(func(ns.NetNS) error {
		allLinks, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("LinkList() failed: %v", err)
		}
		csn, err = nettools.SetupContainerSideNetwork(info, vnt.contNS.Path(), allLinks)
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

	outerIP := addAddress(t, vnt.hostNS, hostVeth, outerAddr)
	vnt.connectTaps(csn.Fds[0])
	// tcpdump should catch udp 'ping' but should not
	// see BOOTP/DHCP on the 'outer' link
	vnt.addTcpdump(hostVeth, "10.1.90.1.4243 > 10.1.90.5.4242: UDP", "BOOTP/DHCP")
	vnt.verifyDhcp(info,
		"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
		"new_ip_address='10.1.90.5'",
		"new_network_number='10.1.90.0'",
		"new_routers='10.1.90.1'",
		"new_subnet_mask='255.255.255.0'",
		"tap0: offered 10.1.90.5 from 169.254.254.2")
	vnt.verifyPing(outerIP)
	vnt.wait()
}

type pinger struct {
	localIP, destIP net.IP
	conn            *net.UDPConn
}

var _ NetTester = &pinger{}

func newPinger(localIP net.IP, destIP net.IP) *pinger {
	return &pinger{localIP: localIP, destIP: destIP}
}

func (p *pinger) Name() string {
	return fmt.Sprintf("pinger for %v", p.destIP)
}

func (p *pinger) Fg() bool { return false }

func (p *pinger) dial() error {
	var laddr *net.UDPAddr
	if p.localIP != nil {
		laddr = &net.UDPAddr{IP: p.localIP, Port: pingSrcPort}
	}
	raddr := &net.UDPAddr{IP: p.destIP, Port: pingPort}
	var err error
	p.conn, err = net.DialUDP("udp4", laddr, raddr)
	if err != nil {
		return fmt.Errorf("net.DialUDP(): %v", err)
	}
	return nil
}

func (p *pinger) ping() error {
	if _, err := p.conn.Write([]byte("hello")); err != nil {
		return fmt.Errorf("Write(): %v", err)
	}
	return nil
}

func (p *pinger) Run(readyCh, stopCh chan struct{}) error {
	if err := p.dial(); err != nil {
		return err
	}
	close(readyCh)
	for {
		select {
		case <-stopCh:
			p.conn.Close()
			return nil
		case <-time.After(pingInterval):
			if err := p.ping(); err != nil {
				// receiver may not be ready yet, don't fail here
				glog.V(3).Infof("ping error: %v", err)
			}
		}
	}
}

type pingReceiver struct {
	localIP net.IP
	conn    *net.UDPConn
}

var _ NetTester = &pingReceiver{}

func newPingReceiver(localIP net.IP) *pingReceiver {
	return &pingReceiver{localIP: localIP}
}

func (p *pingReceiver) Name() string {
	return fmt.Sprintf("pingReceiver for %v", p.localIP)
}

func (p *pingReceiver) Fg() bool { return true }

func (p *pingReceiver) listen() error {
	var err error
	p.conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: p.localIP, Port: pingPort})
	if err != nil {
		return fmt.Errorf("net.ListenUDP(): %v", err)
	}
	return nil
}

func (p *pingReceiver) cycle() (bool, error) {
	if err := p.conn.SetDeadline(time.Now().Add(pingDeadline)); err != nil {
		return false, err
	}
	buf := make([]byte, 16)
	n, addr, err := p.conn.ReadFromUDP(buf)
	if err != nil {
		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			return false, nil
		}
		return false, err
	}
	glog.V(3).Infof("received udp ping from %v: %q", addr, string(buf[:n]))
	return true, nil
}

func (p *pingReceiver) Run(readyCh, stopCh chan struct{}) error {
	if err := p.listen(); err != nil {
		return err
	}
	close(readyCh)
	defer p.conn.Close()
	for n := 0; n < pingReceiverCycles; n++ {
		select {
		case <-stopCh:
			return errors.New("ping receiver stopped before receiving any pings")
		default:
		}
		received, err := p.cycle()
		if err != nil {
			return err
		}
		if received {
			return nil
		}
	}
	return errors.New("no pings received")
}

type safeBuf struct {
	m sync.Mutex
	b bytes.Buffer
}

func (sb *safeBuf) Write(p []byte) (n int, err error) {
	sb.m.Lock()
	defer sb.m.Unlock()
	return sb.b.Write(p)
}

func (sb *safeBuf) String() string {
	sb.m.Lock()
	defer sb.m.Unlock()
	return sb.b.String()
}

type tcpdump struct {
	link           netlink.Link
	stopOn, failOn string
	readyCh        chan struct{}
	bOut, bErr     safeBuf
	cmd            *exec.Cmd
	out            string
}

var _ NetTester = &tcpdump{}

func newTcpdump(link netlink.Link, stopOn, failOn string) *tcpdump {
	return &tcpdump{
		link:    link,
		stopOn:  stopOn,
		failOn:  failOn,
		readyCh: make(chan struct{}),
	}
}

func (t *tcpdump) Name() string {
	return fmt.Sprintf("tcpdump on %s", t.link.Attrs().Name)
}

func (t *tcpdump) Fg() bool { return true }

func (t *tcpdump) gotListeningMsg() bool {
	idx := strings.Index(t.bErr.String(), "\nlistening on")
	if idx < 0 {
		return false
	}
	return strings.LastIndex(t.bErr.String(), "\n") > idx
}

func (t *tcpdump) waitForReady() bool {
	for n := 0; n < tcpdumpStartupPollCount; n++ {
		time.Sleep(tcpdumpPollPeriod)
		if t.gotListeningMsg() {
			// XXX: the correct way would be to wait for
			// some traffic here
			time.Sleep(100 * time.Millisecond)
			return true
		}
	}
	return false
}

func (t *tcpdump) waitForSubstring(stopCh chan struct{}) error {
	for n := 0; n < tcpdumpSubstringWaitCount; n++ {
		select {
		case <-stopCh:
			return fmt.Errorf("tcpdump stopped before producing expected output %q, out:\n", t.stopOn, t.bOut.String())
		case <-time.After(tcpdumpPollPeriod):
			s := t.bOut.String()
			if strings.Contains(s, t.failOn) {
				return fmt.Errorf("found unexpected %q in tcpdump output:\n%s", t.failOn, s)
			}
			if strings.Contains(s, t.stopOn) {
				return nil
			}
		}
	}
	return fmt.Errorf("timed out waiting for tcpdump output %q, out:\n%s", t.stopOn, t.bOut.String())
}

func (t *tcpdump) stop() error {
	// tcpdump exits with status 0 on SIGINT
	// (if it hasn't exited already)
	t.cmd.Process.Signal(os.Interrupt)
	err := t.cmd.Wait()
	if err != nil {
		glog.Errorf("tcpdump failed: %v", err)
	}
	return err
}

func (t *tcpdump) Run(readyCh, stopCh chan struct{}) error {
	// -l stands for 'line buffered'. We want to receive tcpdump
	// output as soon as it's generated
	t.cmd = exec.Command("tcpdump", "-l", "-n", "-i", t.link.Attrs().Name, "udp")
	t.cmd.Stdout = &t.bOut
	t.cmd.Stderr = &t.bErr

	// make sure we start the process in current network namespace
	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tcpdump: %v", err)
	}

	if t.waitForReady() {
		close(readyCh)
	} else {
		t.stop()
		return fmt.Errorf("timed out waiting for tcpdump, error output:\n%s", t.bErr.String())
	}

	if err := t.waitForSubstring(stopCh); err != nil {
		t.stop()
		return err
	}

	err := t.stop()
	t.out = t.bOut.String()
	return err
}

func addAddress(t *testing.T, netNS ns.NetNS, link netlink.Link, addr string) net.IP {
	parsedAddr, err := netlink.ParseAddr(addr)
	if err != nil {
		t.Fatalf("failed to parse snooping address: %v", err)
	}
	if err := netNS.Do(func(ns.NetNS) (err error) {
		return netlink.AddrAdd(link, parsedAddr)
	}); err != nil {
		t.Fatalf("failed to add address to snooping veth: %v", err)
	}
	return parsedAddr.IP
}

// tapConnector copies frames between tap interfaces. It returns
// a channel that should be closed to stop copying and close
// the tap devices
type tapConnector struct {
	tapA, tapB *os.File
	wg         sync.WaitGroup
}

var _ NetTester = &tapConnector{}

func newTapConnector(tapA, tapB *os.File) *tapConnector {
	return &tapConnector{tapA: tapA, tapB: tapB}
}

func (tc *tapConnector) Name() string { return "tapConnector" }
func (tc *tapConnector) Fg() bool     { return false }

// copyFrames copies a packets between tap devices. Unlike
// io.Copy(), it doesn't use buffering and thus keeps frame boundaries
func (tc *tapConnector) copyFrames(from, to *os.File) {
	buf := make([]byte, 1600)
	for {
		nRead, err := from.Read(buf)
		if err != nil {
			glog.Infof("copyFrames(): Read(): %v", err)
			break
		}
		nWritten, err := to.Write(buf[:nRead])
		if err != nil {
			glog.Infof("copyFrames(): Write(): %v", err)
			break
		}
		if nWritten < nRead {
			glog.Warning("copyFrames(): short Write(): %d bytes instead of %d", nWritten, nRead)
		}
	}
	tc.wg.Done()
}

func (tc *tapConnector) Run(readyCh, stopCh chan struct{}) error {
	tc.wg.Add(1)
	go tc.copyFrames(tc.tapA, tc.tapB)
	tc.wg.Add(1)
	go tc.copyFrames(tc.tapB, tc.tapA)
	close(readyCh)
	<-stopCh
	// TODO: use SetDeadline() when it's available for os.File
	// (perhaps in Go 1.10): https://github.com/golang/go/issues/22114
	tc.tapA.Close()
	tc.tapB.Close()
	tc.wg.Wait()
	return nil
}

// TODO: document NetTester / NetTestGroup
// TODO: block ip trafic from br0 ip

// TODO: apply CNI result using ConfigureLink()
// TODO: use https://github.com/d4l3k/messagediff to diff cni results
// TODO: test network teardown
// TODO: test multiple CNIs
// TODO: test Calico
