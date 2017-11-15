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
)

func TestVmNetwork(t *testing.T) {
	hostNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create host ns: %v", err)
	}
	defer hostNS.Close()

	contNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create container ns: %v", err)
	}
	defer contNS.Close()

	clientNS, err := ns.NewNS()
	if err != nil {
		t.Fatalf("Failed to create ns for dhcp client: %v", err)
	}

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

	var dhcpClientVeth, hostVeth netlink.Link
	if err := hostNS.Do(func(ns.NetNS) (err error) {
		hostVeth, _, err = nettools.CreateEscapeVethPair(contNS, "eth0", 1500)
		return
	}); err != nil {
		t.Fatalf("failed to create escape veth pair: %v", err)
	}

	if err := contNS.Do(func(ns.NetNS) error {
		_, err = nettools.SetupContainerSideNetwork(info, contNS.Path())
		if err != nil {
			return fmt.Errorf("failed to set up container side network: %v", err)
		}

		// Here we setup extra veth for dhcp client.
		// That's temporary solution, what we really should
		// use is another tap interface + forwarding.
		// See https://nsl.cz/using-tun-tap-in-go-or-how-to-write-vpn/
		// Also https://ldpreload.com/p/vpn-with-socat.txt
		var vethToBridge netlink.Link
		vethToBridge, dhcpClientVeth, err = nettools.CreateEscapeVethPair(clientNS, "veth0", 1500)
		if err != nil {
			return fmt.Errorf("failed to create veth pair for the client: %v", err)
		}

		brLink, err := netlink.LinkByName("br0")
		if err != nil {
			return fmt.Errorf("failed to locate container-side bridge: %v", err)
		}

		br, ok := brLink.(*netlink.Bridge)
		if !ok {
			return fmt.Errorf("br0 is not a bridge: %#v", brLink)
		}

		if err := netlink.LinkSetMaster(vethToBridge, br); err != nil {
			return fmt.Errorf("failed to set master for dhcp client veth: %v", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("failed to set up container-side network: %v", err)
	}
	if err := clientNS.Do(func(ns.NetNS) error {
		mac, _ := net.ParseMAC(clientMacAddress)
		if err = nettools.SetHardwareAddr(dhcpClientVeth, mac); err != nil {
			return fmt.Errorf("can't set test MAC address on client interface: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	outerIP := addAddress(t, hostNS, hostVeth, outerAddr)

	g := NewNetTestGroup(t, 15*time.Second)
	defer g.Stop()

	// tcpdump should catch udp 'ping' but should not
	// see BOOTP/DHCP on the 'outer' link
	tcpdump := newTcpdump(hostVeth, "10.1.90.1.4243 > 10.1.90.5.4242: UDP", "BOOTP/DHCP")
	g.Add(hostNS, tcpdump)

	g.Add(contNS, NewDhcpServerTester(info))
	// wait for dhcp client to complete so we don't interfere
	// with the network link too early
	<-g.Add(clientNS, NewDhcpClient([]string{
		"new_classless_static_routes='10.10.42.0/24 10.1.90.90'",
		"new_ip_address='10.1.90.5'",
		"new_network_number='10.1.90.0'",
		"new_routers='10.1.90.1'",
		"new_subnet_mask='255.255.255.0'",
		"veth0: offered 10.1.90.5 from 169.254.254.2",
	}))

	// dhcpcd -T doesn't add address to the link
	clientIP := addAddress(t, clientNS, dhcpClientVeth, clientAddr)
	g.Add(hostNS, newPinger(outerIP, clientIP))
	g.Add(clientNS, newPinger(clientIP, outerIP))
	g.Add(clientNS, newPingReceiver(clientIP))
	g.Add(hostNS, newPingReceiver(outerIP))

	g.Wait()
}

type pinger struct {
	localIP, destIP net.IP
	conn            *net.UDPConn
}

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

// TODO: document NetTester / NetTestGroup
// TODO: block ip trafic from br0 ip
