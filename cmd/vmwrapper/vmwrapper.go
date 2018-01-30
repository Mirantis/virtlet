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
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	fdSocketPath    = "/var/lib/virtlet/tapfdserver.sock"
	defaultEmulator = "/usr/bin/qemu-system-x86_64" // FIXME
	emulatorVar     = "VIRTLET_EMULATOR"
	netKeyEnvVar    = "VIRTLET_NET_KEY"
	vmsProcFile     = "/var/lib/virtlet/vms.procfile"
)

func extractLastUsedPCIAddress(args []string) int {
	var lastUsed int
	for _, arg := range args {
		i := strings.LastIndex(arg, "addr=0x")
		if i < 0 {
			continue
		}
		parsed, _ := strconv.ParseInt(arg[i+7:], 16, 32)
		lastUsed = int(parsed)
	}
	return lastUsed
}

type reexecArg struct {
	Args []string
}

func handleReexec(arg interface{}) (interface{}, error) {
	args := arg.(*reexecArg).Args
	if err := syscall.Exec(args[0], args, os.Environ()); err != nil {
		return nil, fmt.Errorf("Can't exec emulator: %v", err)
	}
	return nil, nil // unreachable
}

func main() {
	utils.RegisterNsFixReexec("vmwrapper", handleReexec, reexecArg{})
	utils.HandleNsFixReexec()

	// configure glog (apparently no better way to do it ...)
	flag.CommandLine.Parse([]string{"-v=3", "-alsologtostderr=true"})

	runInAnotherContainer := os.Getuid() != 0

	var pid int
	var err error
	if runInAnotherContainer {
		glog.V(0).Infof("Obtaining PID of the VM container process...")
		pid, err = utils.WaitForProcess(vmsProcFile)
		if err != nil {
			glog.Errorf("Can't obtain PID of the VM container process")
			os.Exit(1)
		}
	}

	emulator := os.Getenv(emulatorVar)
	emulatorArgs := os.Args[1:]
	var netArgs []string
	if emulator == "" {
		// this happens during 'qemu -help' invocation by libvirt
		// (capability check)
		emulator = defaultEmulator
	} else {
		netFdKey := os.Getenv(netKeyEnvVar)
		nextToUsePCIAddress := extractLastUsedPCIAddress(os.Args[1:]) + 1
		nextToUseHostdevNo := 0

		if netFdKey != "" {
			c := tapmanager.NewFDClient(fdSocketPath)
			if err := c.Connect(); err != nil {
				glog.Errorf("Can't connect to fd server: %v", err)
				os.Exit(1)
			}
			fds, marshaledData, err := c.GetFDs(netFdKey)
			if err != nil {
				glog.Errorf("Failed to obtain tap fds for key %q: %v", netFdKey, err)
				os.Exit(1)
			}

			var descriptions []tapmanager.InterfaceDescription
			if err := json.Unmarshal(marshaledData, &descriptions); err != nil {
				glog.Errorf("Failed to unmarshal network interface info", err)
				os.Exit(1)
			}

			for i, desc := range descriptions {
				switch desc.Type {
				case network.InterfaceTypeTap:
					netArgs = append(netArgs,
						"-netdev",
						fmt.Sprintf("tap,id=tap%d,fd=%d", desc.FdIndex, fds[desc.FdIndex]),
						"-device",
						fmt.Sprintf("virtio-net-pci,netdev=tap%d,id=net%d,mac=%s", desc.FdIndex, i, desc.HardwareAddr),
					)
				case network.InterfaceTypeVF:
					netArgs = append(netArgs,
						"-device",
						// fmt.Sprintf("pci-assign,configfd=%d,host=%s,id=hostdev%d,bus=pci.0,addr=0x%x",
						fmt.Sprintf("pci-assign,host=%s,id=hostdev%d,bus=pci.0,addr=0x%x",
							// desc.FdIndex,
							desc.PCIAddress[5:],
							nextToUseHostdevNo,
							nextToUsePCIAddress,
						),
					)
					nextToUseHostdevNo += 1
					nextToUsePCIAddress += 1
				default:
					// Impssible situation when tapmanager is built from other sources than vmwrapper
					glog.Errorf("Received unknown interface type: %d", int(desc.Type))
					os.Exit(1)
				}
			}
		}
	}

	args := append([]string{emulator}, emulatorArgs...)
	args = append(args, netArgs...)
	env := os.Environ()
	if runInAnotherContainer {
		// Currently we don't drop privs when SR-IOV support is enabled
		// because of an unresolved emulator permission problem.
		dropPrivs := os.Getenv("VMWRAPPER_KEEP_PRIVS") == ""
		if err := utils.SwitchToNamespaces(pid, "vmwrapper", &reexecArg{args}, dropPrivs); err != nil {
			glog.Fatalf("Error reexecuting vmwrapper: %v", err)
		}
	} else {
		// this log hides errors returned by libvirt virError
		// because of libvirt's output parsing approach
		// glog.V(0).Infof("Executing emulator: %s", strings.Join(args, " "))
		if err := syscall.Exec(args[0], args, env); err != nil {
			glog.Errorf("Can't exec emulator: %v", err)
			os.Exit(1)
		}
	}
}
