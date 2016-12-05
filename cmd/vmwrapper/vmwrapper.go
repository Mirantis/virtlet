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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/dhcp"
	"github.com/Mirantis/virtlet/pkg/nettools"
)

const (
	nsEnvVar         = "VIRTLET_NS"
	peerHwAddrEnvVar = "VIRTLET_HWADDR"
	cniConfigEnvVar  = "VIRTLET_CNI_CONFIG"
)

func main() {
	var info *types.Result

	// XXX: rm
	flag.Set("v", "3")
	flag.Set("alsologtostderr", "true")
	flag.Parse()

	if len(os.Args) < 2 {
		glog.Errorf("must specify emulator executable")
		os.Exit(1)
	}
	emulator := os.Args[1]
	emulatorArgs := os.Args[2:]

	nsPath := os.Getenv(nsEnvVar)
	if nsPath == "" {
		glog.Errorf("must specify VIRTLET_NS")
		os.Exit(1)
	}

	vmNS, err := ns.GetNS(nsPath)
	if err != nil {
		glog.Errorf("failed to open network namespace at %q: %v", nsPath, err)
		os.Exit(1)
	}

	peerHwAddrStr := os.Getenv(peerHwAddrEnvVar)
	if peerHwAddrStr == "" {
		glog.Errorf("must specify VIRTLET_HWADDR")
		os.Exit(1)
	}
	peerHwAddr, err := net.ParseMAC(peerHwAddrStr)
	if err != nil {
		glog.Errorf("bad hwaddr in VIRTLET_HWADDR")
		os.Exit(1)
	}

	cniConfig := os.Getenv(cniConfigEnvVar)
	if cniConfig != "" {
		if err := json.Unmarshal([]byte(cniConfig), &info); err != nil {
			glog.Errorf("failed to unmarshal cni config: %v", err)
			os.Exit(1)
		}
	}

	if err := vmNS.Do(func(ns.NetNS) error {
		info, err = nettools.SetupContainerSideNetwork(info)
		if err != nil {
			return err
		}
		dhcpConfg := &dhcp.Config{
			CNIResult:           *info,
			PeerHardwareAddress: peerHwAddr,
		}
		dhcpServer := dhcp.NewServer(dhcpConfg)
		if err := dhcpServer.SetupListener("0.0.0.0"); err != nil {
			return fmt.Errorf("failed to set up dhcp listener: %v", err)
		}

		errCh := make(chan error)
		go func() {
			errCh <- dhcpServer.Serve()
		}()

		cmd := exec.Command(emulator, emulatorArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start %q: %v", emulator, err)
		}
		err := cmd.Wait()
		select {
		case dhcpErr := <-errCh:
			if dhcpErr == nil {
				glog.Errorf("dhcp server self-exited with nil error")
			} else {
				glog.Errorf("dhcp server failed: %v", dhcpErr)
			}
		default:
		}

		if err != nil {
			return fmt.Errorf("%q failed: %v", emulator, err)
		}

		return nil
	}); err != nil {
		glog.Error(err)
		os.Exit(1)
	}
}
