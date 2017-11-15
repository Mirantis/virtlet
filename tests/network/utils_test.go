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
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"

	"github.com/Mirantis/virtlet/pkg/dhcp"
)

const (
	maxNetTestMembers = 32
	dhcpcdTimeout     = 2
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
		err := netNS.Do(func(ns.NetNS) (err error) {
			return tester.Run(readyCh, g.stopCh)
		})
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
	config *cnicurrent.Result
}

func NewDhcpServerTester(config *cnicurrent.Result) *DhcpServerTester {
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
	expectedSubstrings []string
}

func NewDhcpClient(expectedSubstrings []string) *DhcpClient {
	return &DhcpClient{expectedSubstrings}
}

func (d *DhcpClient) Name() string { return "dhcp client" }
func (d *DhcpClient) Fg() bool     { return true }

func (d *DhcpClient) Run(readyCh, stopCh chan struct{}) error {
	args := []string{"-T", "-t", strconv.Itoa(dhcpcdTimeout)}
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
