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
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/tapmanager"
)

const (
	fdSocketPath    = "/var/lib/virtlet/tapfdserver.sock"
	defaultEmulator = "/usr/bin/qemu-system-x86_64" // FIXME
	emulatorVar     = "VIRTLET_EMULATOR"
	netKeyEnvVar    = "VIRTLET_NET_KEY"
)

// Spawn child process and wait for its termination.
// If process receives EOF on pipe from parent or SIGTERM
// it forwards SIGKILL or SIGTERM to child accordingly.
func runCommand(name string, args []string, exitEOF chan bool, sigTERM chan os.Signal, tapFile *os.File) (error, bool) {
	var err error
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if len(args) >= 1 && args[0] == "-child" && tapFile != nil {
		glog.Error("Unexpected combination of input args: '-child' and tapFile are set simultaneously")
		return errors.New("Unexpected combination of input args: '-child' and tapFile are set simultaneously"), false
	}

	if tapFile != nil {
		cmd.ExtraFiles = []*os.File{
			tapFile,
		}
		defer tapFile.Close()
	}

	if len(args) >= 1 && args[0] == "-child" {
		pipeR, _, err := os.Pipe()
		if err != nil {
			glog.Errorf("Failed to create pipe: %v", err)
		}
		cmd.ExtraFiles = []*os.File{
			pipeR,
		}
		defer pipeR.Close()
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
				glog.Info("Received EOF on pipe from parent process. Forwarding to child SIGKILL.")
			} else {
				glog.Info("Unexpected error on read pipe from parent process. Forwarding to child SIGKILL.")
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

func runCommandIfExists(path string, args []string, exitEOF chan bool, sigTERM chan os.Signal, tapFile *os.File) (error, bool) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return runCommand(path, args, exitEOF, sigTERM, tapFile)
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
	emulatorArgs := os.Args[1:]

	if emulator == "" {
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
	runCommandIfExists("/vmwrapper", childArgs, exitEOF, sigTERM, nil)
}

func child(exitEOF chan bool, sigTERM chan os.Signal) {
	go catchParentKilled(exitEOF)

	emulatorArgs := os.Args[2:]

	// FIXME
	os.Args = []string{os.Args[0], "-v=3", "-alsologtostderr=true"}
	flag.Parse()

	emulator := os.Getenv(emulatorVar)
	netFdKey := os.Getenv(netKeyEnvVar)

	// TODO: use cleaner way to do this
	runHook := func(name string) {
		err, procTerminated := runCommandIfExists(name, append([]string{emulator, netFdKey}, emulatorArgs...), exitEOF, sigTERM, nil)
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

	c := tapmanager.NewFDClient(fdSocketPath)
	if err := c.Connect(); err != nil {
		glog.Errorf("Can't connect to fd server: %v", err)
		os.Exit(1)
	}
	tapFd, hwAddr, err := c.GetFD(netFdKey)
	if err != nil {
		glog.Errorf("Failed to obtain tap fd for key %q: %v", netFdKey, err)
		os.Exit(1)
	}

	tapFile := os.NewFile(uintptr(tapFd), "acquired-fd")
	defer tapFile.Close()

	netArgs := []string{
		"-netdev",
		// Qemu process is started with 3d FD set to already opened TAP device
		"tap,id=tap0,fd=3",
		"-device",
		"virtio-net-pci,netdev=tap0,id=net0,mac=" + net.HardwareAddr(hwAddr).String(),
	}

	// On domain's shutdown/destroy libvirt terminates corresponding qemu process using SIGTERM or SIGKILL.
	// SIGKILL is detected by catching EOF on a pipe from parent process.
	// On catching SIGTERM just forward it to qemu process and wait for it to exit.

	err, _ = runCommand(emulator, append(emulatorArgs, netArgs...), exitEOF, sigTERM, tapFile)
	runHook("/vmwrapper-after-qemu.sh")
	if err != nil {
		glog.Errorf("Error occurred while starting subprocess in Virtlet base network namespace: %v", err)
		os.Exit(1)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

	exitEOF := make(chan bool, 1)
	sigTERM := make(chan os.Signal)
	signal.Notify(sigTERM, syscall.SIGTERM)

	if len(os.Args) > 1 && os.Args[1] == "-child" {
		child(exitEOF, sigTERM)
	} else {
		parent(exitEOF, sigTERM)
	}
}

// TODO: convert to syscall.Exec
