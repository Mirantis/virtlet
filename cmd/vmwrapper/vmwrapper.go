/*
Copyright 2016-2017 Mirantis

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

// Spawn child process and wait for its termination.
// If process receives EOF on pipe from parent or SIGTERM
// it forwards SIGKILL or SIGTERM to child accordingly.
func runCommand(name string, args []string, exitEOF chan bool, sigTERM chan os.Signal) (error, bool) {
	var err error
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	pipeR, _, _ := os.Pipe()
	if len(args) >= 1 && args[0] == "-child" {
		cmd.ExtraFiles = []*os.File{
			pipeR,
		}
	}

	if err = cmd.Start(); err != nil {
		glog.Errorf("Failed to start %q: %v", name, err)
		return err, false
	}

	done := make(chan bool, 1)
	go func() {
		if err = cmd.Wait(); err != nil {
			glog.Errorf("Failed to run %q: %v", name, err)
		}
		done <- true
	}()
	procTerminated := false

L:
	for {
		select {
		case <-done:
			break L
		case ret := <-exitEOF:
			if ret {
				glog.Infof("Received EOF on pipe from parent process. Forwarding to child SIGKILL.")
			} else {
				glog.Infof("Unexpected error on read pipe from parent process. Forwarding to child SIGKILL.")
			}
			if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
				glog.Errorf("Error forwarding SIGKILL to child %v.", err)
			}
			procTerminated = true
		case <-sigTERM:
			glog.Infof("Received SIGTERM. Forwarding to child and wait the process to stop.")
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				glog.Errorf("Error forwarding SIGTERM to child %v.", err)
			}
			procTerminated = true
		}
	}
	return err, procTerminated
}

func runCommandIfExists(path string, args []string, exitEOF chan bool, sigTERM chan os.Signal) (error, bool) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return runCommand(path, args, exitEOF, sigTERM)
	case os.IsNotExist(err):
		return nil, false
	default:
		return fmt.Errorf("os.Stat(): %v", err), false
	}
}

func catchParentKilled(exitEOF chan bool) {
	// Child process is connected to read endpoint of pipe, on 3rd file descriptor.
	// In loop, try to read something from pipe until it's closed by second side.
	pipeR := os.NewFile(uintptr(3), "pipe")

	data := make([]byte, 1)
	for {
		_, err := pipeR.Read(data)
		if err == io.EOF {
			break
		} else if err != nil {
			glog.Errorf("Read from pipe error: %v", err)
			exitEOF <- false
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	exitEOF <- true
}

// The purpose of this parent process is to be able to do cleanup
// in case of libvirt terminates process with SIGKILL.
// It is done using pipe with read end passed to child,
// which watches for EOF on the pipe.

func parent(exitEOF chan bool, sigTERM chan os.Signal) {
	emulator := os.Getenv(emulatorVar)
	nsPath := os.Getenv(nsEnvVar)
	emulatorArgs := os.Args[1:]

	if emulator == "" || nsPath == "" {
		// this happens during qemu -help invocation by virtlet
		// (capability check)
		// TODO: only do this when *all* above vars are empty
		// TODO: use per-emulator symlinks to vmwrapper to determine
		// the necessary binary instead of using fixed defaultEmulator
		// here
		// Exec'ing as cleanup is not need in this case
		emulatorArgs := append([]string{defaultEmulator}, emulatorArgs...)
		env := os.Environ()
		if err := syscall.Exec(defaultEmulator, emulatorArgs, env); err != nil {
			glog.Errorf("Emulator %q failed: %v", defaultEmulator, err)
			os.Exit(1)
		}
		// Wrapper process ends here and is replaced by qemu process
	}

	childArgs := append([]string{"-child"}, emulatorArgs...)
	runCommandIfExists("/vmwrapper", childArgs, exitEOF, sigTERM)
}

func child(exitEOF chan bool, sigTERM chan os.Signal) {
	var info *types.Result

	go catchParentKilled(exitEOF)

	emulatorArgs := os.Args[2:]

	// FIXME
	os.Args = []string{os.Args[0], "-v=3", "-alsologtostderr=true"}
	flag.Parse()

	emulator := os.Getenv(emulatorVar)
	nsPath := os.Getenv(nsEnvVar)

	// TODO: use cleaner way to do this
	runHook := func(name string) {
		err, procTerminated := runCommandIfExists(name, append([]string{emulator, nsPath, os.Getenv(cniConfigEnvVar)}, emulatorArgs...), exitEOF, sigTERM)
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
			return fmt.Errorf("Failed to set up dhcp listener: %v", err)
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

		// Running qemu process
		// On domain's shutdown/destroy libvirt terminates corresponding qemu process using SIGTERM or SIGKILL.
		// SIGKILL is detected by catching EOF on a pipe from parent process.
		// On catching SIGTERM just forward it to qemu process and wait for it to exit.
		err, _ := runCommand(emulator, append(emulatorArgs, netArgs...), exitEOF, sigTERM)

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

func main() {
	exitEOF := make(chan bool, 1)
	sigTERM := make(chan os.Signal)
	signal.Notify(sigTERM, syscall.SIGTERM)

	if len(os.Args) > 1 && os.Args[1] == "-child" {
		child(exitEOF, sigTERM)
	} else {
		parent(exitEOF, sigTERM)
	}
}
