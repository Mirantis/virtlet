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
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/dhcp"
	"github.com/Mirantis/virtlet/pkg/nettools"
)

const (
	defaultEmulator = "/usr/bin/qemu-system-x86_64" // FIXME
	emulatorVar     = "VIRTLET_EMULATOR"
	nsEnvVar        = "VIRTLET_NS"
	cniConfigEnvVar = "VIRTLET_CNI_CONFIG"
)

var exitEOF = make(chan bool, 1)
var sigTERM = make(chan os.Signal)

//Spawn child process and wait for its termination
//If during waiting for the child termination process
//will recieve EOF on pipe from parent or SIGTERM
//it will send SIGKILL or SIGTERM to child accordingly
func runCommand(name string, args []string) (error, bool) {
	var err error
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err = cmd.Start(); err != nil {
		glog.Errorf("failed to start %q: %v", name, err)
		return err, false
	}

	done := make(chan bool, 1)
	go func() {
		if err = cmd.Wait(); err != nil {
			glog.Errorf("failed to run %q: %v", name, err)
		}
		done <- true
	}()
	procTerminated := false

L:
	for {
		select {
		case <-done:
			break L
		case <-exitEOF:
			glog.Infof("Recieved EOF on pipe from outer wrapper. Sending to child SIGKILL.\n")
			if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
				glog.Infof("Error on sending SIGKILL to child %v!\n", err)
			}
			procTerminated = true
		case <-sigTERM:
			glog.Infof("Recieved SIGTERM. Sending to child and wait the process to stop.!\n")
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				glog.Infof("Error on sending SIGTERM to child %v!\n", err)
			}
			procTerminated = true
		}
	}
	return err, procTerminated
}

func runCommandIfExists(path string, args []string) (error, bool) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return runCommand(path, args)
	case os.IsNotExist(err):
		return nil, false
	default:
		return fmt.Errorf("os.Stat(): %v", err), false
	}
}

func catchParentKilled() {
	//Process has been spawned by outer wrapper with setting for child
	//file descriptor of the read end of created pipe to the '3'
	pipeR := os.NewFile(uintptr(3), "pipe")

	data := make([]byte, 1)
	for {
		_, err := pipeR.Read(data)
		if err == io.EOF {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	exitEOF <- true
}

func main() {
	var info *types.Result

	go catchParentKilled()
	signal.Notify(sigTERM, syscall.SIGTERM)

	emulatorArgs := os.Args[1:]

	// FIXME
	os.Args = []string{os.Args[0], "-v=3", "-alsologtostderr=true"}
	flag.Parse()

	emulator := os.Getenv(emulatorVar)
	nsPath := os.Getenv(nsEnvVar)

	if emulator == "" || nsPath == "" {
		// this happens during qemu -help invocation by virtlet
		// (capability check)
		// TODO: only do this when *all* above vars are empty
		// TODO: use per-emulator symlinks to vmwrapper to determine
		// the necessary binary instead of using fixed defaultEmulator
		// here
		if err, _ := runCommand(defaultEmulator, emulatorArgs); err != nil {
			glog.Errorf("Emulator %q failed: %v", defaultEmulator, err)
			os.Exit(1)
		}
		return
	}

	// TODO: use cleaner way to do this
	runHook := func(name string) {
		err, procTerminated := runCommandIfExists(name, append([]string{emulator, nsPath, os.Getenv(cniConfigEnvVar)}, emulatorArgs...))
		if err != nil {
			glog.Errorf("Hook %q: %v", name, err)
			if procTerminated {
				os.Exit(1)
			}
		}
		if procTerminated {
			glog.Errorf("Hook %q was terminated either forcibly or gracefully. Terminate execution.", name)
			os.Exit(0)
		}
	}

	runHook("/vmwrapper-entry.sh")

	vmNS, err := ns.GetNS(nsPath)
	if err != nil {
		glog.Errorf("Failed to open network namespace at %q: %v", nsPath, err)
		os.Exit(1)
	}

	peerHwAddr, err := nettools.GenerateMacAddress()
	if err != nil {
		glog.Errorf("Failed to generate mac address: %v", err)
		os.Exit(1)
	}

	cniConfig := os.Getenv(cniConfigEnvVar)
	if cniConfig != "" {
		if err := json.Unmarshal([]byte(cniConfig), &info); err != nil {
			glog.Errorf("Failed to unmarshal cni config: %v", err)
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

		runHook("/vmwrapper-pre-qemu.sh")

		errCh := make(chan error)
		go func() {
			// FIXME: do this cleaner
			errCh <- vmNS.Do(func(ns.NetNS) error {
				return dhcpServer.Serve()
			})
		}()

		netArgs := []string{
			"-netdev",
			"tap,id=tap0,ifname=tap0,script=no,downscript=no",
			"-device",
			"virtio-net-pci,netdev=tap0,id=net0,mac=" + peerHwAddr.String(),
		}

		//Running qemu process
		//On domain's shutdown/destroy libvirt terminates corresponding qemu process by using SIGTERM or SIGKILL
		//SIGKILL will be detected by catching up EOF on pipe from outer wrapper designed only with this single gone
		//On catching SIGTERM just re-send it to spawned qemu process and wait for its termination
		err, _ := runCommand(emulator, append(emulatorArgs, netArgs...))

		select {
		case dhcpErr := <-errCh:
			if dhcpErr == nil {
				glog.Errorf("DHCP server self-exited with nil error")
			} else {
				glog.Errorf("DHCP server failed: %v", dhcpErr)
			}
		default:
		}

		if err := nettools.TeardownContainerSideNetwork(info); err != nil {
			return err
		}

		runHook("/vmwrapper-after-qemu.sh")

		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		glog.Error("Error occurred while in VM network namespace: %s", err)
		os.Exit(1)
	}
}
