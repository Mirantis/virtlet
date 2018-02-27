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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/golang/glog"
	"github.com/vishvananda/netlink"

	"github.com/Mirantis/virtlet/pkg/dhcp"
	"github.com/Mirantis/virtlet/pkg/network"
)

const (
	maxNetTestMembers         = 32
	dhcpcdTimeout             = 2
	pingPort                  = 4242
	pingSrcPort               = 4243
	pingInterval              = 100 * time.Millisecond
	pingDeadline              = 55 * time.Millisecond
	pingReceiverCycles        = 100
	tcpdumpPollPeriod         = 50 * time.Millisecond
	tcpdumpStartupPollCount   = 100
	tcpdumpSubstringWaitCount = 100
)

type NetTester interface {
	Name() string
	Fg() bool
	Run(readyCh, stopCh chan struct{}) error
}

type NetTestGroup struct {
	wg, fg  sync.WaitGroup
	t       *testing.T
	timeout time.Duration
	stopCh  chan struct{}
	errCh   chan error
	nVacant int
}

func NewNetTestGroup(t *testing.T, timeout time.Duration) *NetTestGroup {
	return &NetTestGroup{
		t:       t,
		timeout: timeout,
		stopCh:  make(chan struct{}),
		errCh:   make(chan error, maxNetTestMembers),
		nVacant: maxNetTestMembers,
	}
}

func (g *NetTestGroup) Add(netNS ns.NetNS, tester NetTester) chan struct{} {
	if g.nVacant == 0 {
		// sending to errCh can possibly block if we add more members
		// than maxNetTestMembers
		g.t.Fatal("can't add more members to the test group")
	}
	doneCh := make(chan struct{})
	g.nVacant--
	readyCh := make(chan struct{})
	g.wg.Add(1)
	if tester.Fg() {
		g.fg.Add(1)
	}
	go func() {
		var err error
		if netNS != nil {
			err = netNS.Do(func(ns.NetNS) (err error) {
				return tester.Run(readyCh, g.stopCh)
			})
		} else {
			err = tester.Run(readyCh, g.stopCh)
		}
		if err != nil {
			g.errCh <- fmt.Errorf("%s: %v", tester.Name(), err)
		}
		if tester.Fg() {
			g.fg.Done()
		}
		g.wg.Done()
		close(doneCh)
	}()
	select {
	case err := <-g.errCh:
		close(g.stopCh)
		g.stopCh = nil
		g.t.Fatal(err)
	case <-readyCh:
	}
	return doneCh
}

func (g *NetTestGroup) Stop() {
	if g.stopCh != nil {
		close(g.stopCh)
		g.stopCh = nil
		g.wg.Wait()
	}
}

func (g *NetTestGroup) Wait() {
	if g.stopCh == nil {
		g.t.Fatalf("test group already stopped")
	}

	var msgs []string
	fgDoneCh := make(chan struct{})
	go func() {
		g.fg.Wait()
		close(fgDoneCh)
	}()
	select {
	case <-fgDoneCh:
	case <-time.After(g.timeout):
		msgs = append(msgs, "test group timed out")
	}

	close(g.stopCh)
	g.stopCh = nil
	g.wg.Wait()
	for {
		select {
		case err := <-g.errCh:
			msgs = append(msgs, err.Error())
		default:
			if len(msgs) > 0 {
				g.t.Fatalf("test group failed:\n%s", strings.Join(msgs, "\n"))
			}
			return
		}
	}
}

type DhcpServerTester struct {
	config *network.ContainerSideNetwork
}

func NewDhcpServerTester(config *network.ContainerSideNetwork) *DhcpServerTester {
	return &DhcpServerTester{config}
}

func (d *DhcpServerTester) Name() string { return "dhcp server" }
func (d *DhcpServerTester) Fg() bool     { return false }

func (d *DhcpServerTester) Run(readyCh, stopCh chan struct{}) error {
	server := dhcp.NewServer(d.config)
	if err := server.SetupListener("0.0.0.0"); err != nil {
		return fmt.Errorf("failed to setup dhcp listener: %v", err)
	}

	close(readyCh)
	go func() {
		<-stopCh
		// If this happens before server.Serve() is executed,
		// Serve() will fail, but no race condition should happen
		server.Close()
	}()
	err := server.Serve()
	select {
	case <-stopCh:
		// skip 'use of closed network connection' error
		// if the server was stopped
		return nil
	default:
		return err
	}
}

type DhcpClient struct {
	iface              string
	expectedSubstrings []string
}

func NewDhcpClient(iface string, expectedSubstrings []string) *DhcpClient {
	return &DhcpClient{iface, expectedSubstrings}
}

func (d *DhcpClient) Name() string { return "dhcp client" }
func (d *DhcpClient) Fg() bool     { return true }

func (d *DhcpClient) Run(readyCh, stopCh chan struct{}) error {
	args := []string{"-T", "-t", strconv.Itoa(dhcpcdTimeout), d.iface}
	cmd := exec.Command("dhcpcd", args...)
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting dhcpcd: %v", err)
	}
	close(readyCh)
	doneCh := make(chan struct{})
	go func() {
		select {
		case <-stopCh:
			cmd.Process.Kill()
		case <-doneCh:
		}
	}()
	err := cmd.Wait()
	close(doneCh)
	outStr := b.String()
	if err != nil {
		return fmt.Errorf("dhcpcd %s failed: %v\nout:\n%s", strings.Join(args, " "), err, outStr)
	}

	var missing []string
	for _, str := range d.expectedSubstrings {
		if !strings.Contains(outStr, str) {
			missing = append(missing, str)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("some of the substrings are missing from dhcpcd output:\n%s\n--- Full output:\n%s",
			strings.Join(missing, "\n"), outStr)
	}
	return nil
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

func parseAddr(t *testing.T, addr string) *netlink.Addr {
	netAddr, err := netlink.ParseAddr(addr)
	if err != nil {
		t.Fatalf("failed to parse snooping address: %v", err)
	}
	return netAddr
}

func addAddress(t *testing.T, netNS ns.NetNS, link netlink.Link, addr string) {
	netAddr := parseAddr(t, addr)
	if err := netNS.Do(func(ns.NetNS) (err error) {
		return netlink.AddrAdd(link, netAddr)
	}); err != nil {
		t.Fatalf("failed to add address to snooping veth: %v", err)
	}
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
// TODO: block ip traffic from br0 ip
