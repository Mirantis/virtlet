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
	"syscall"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/config"
	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/nsfix"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/utils/cgroups"
)

const (
	fdSocketPath    = "/var/lib/virtlet/tapfdserver.sock"
	defaultEmulator = "/usr/bin/qemu-system-x86_64" // FIXME
	vmsProcFile     = "/var/lib/virtlet/vms.procfile"
)

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
	nsfix.RegisterReexec("vmwrapper", handleReexec, reexecArg{})
	nsfix.HandleReexec()

	// configure glog (apparently no better way to do it ...)
	flag.CommandLine.Parse([]string{"-v=3", "-logtostderr=true"})

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

	// FIXME: move the pid of qemu instance out of /kubepods/podxxxxxxx
	// for some cases it will be killed by kubelet after the virtlet pod is deleted/recreated
	if _, err := cgroups.GetProcessController(os.Getpid(), "hugetlb"); err == nil {
		err = cgroups.MoveProcess(os.Getpid(), "hugetlb", "/")
		if err != nil {
			glog.Warningf("failed to move pid into hugetlb path /: %v", err)
		}
	}

	emulator := os.Getenv(config.EmulatorEnvVarName)
	emulatorArgs := os.Args[1:]
	var netArgs []string
	if emulator == "" {
		// this happens during 'qemu -help' invocation by libvirt
		// (capability check)
		emulator = defaultEmulator
	} else {
		netFdKey := os.Getenv(config.NetKeyEnvVarName)
		nextToUseHostdevNo := 0

		if netFdKey != "" {
			c := tapmanager.NewFDClient(fdSocketPath)
			fds, marshaledData, err := c.GetFDs(netFdKey)
			if err != nil {
				glog.Errorf("Failed to obtain tap fds for key %q: %v", netFdKey, err)
				os.Exit(1)
			}

			var descriptions []tapmanager.InterfaceDescription
			if err := json.Unmarshal(marshaledData, &descriptions); err != nil {
				glog.Errorf("Failed to unmarshal network interface info: %v", err)
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
						fmt.Sprintf("vfio-pci,host=%s,id=hostdev%d",
							desc.PCIAddress[5:],
							nextToUseHostdevNo,
						),
					)
					nextToUseHostdevNo += 1
				default:
					// Impossible situation when tapmanager is built from other sources than vmwrapper
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
		nsFixCall := nsfix.NewCall("vmwrapper").
			TargetPid(pid).
			Arg(&reexecArg{args}).
			RemountSys()
		// Currently we don't drop privs when SR-IOV support is enabled
		// because of an unresolved emulator permission problem.
		if os.Getenv("VMWRAPPER_KEEP_PRIVS") == "" {
			nsFixCall.DropPrivs()
		}
		if err := nsFixCall.SwitchToNamespaces(); err != nil {
			glog.Fatalf("Error reexecuting vmwrapper: %v", err)
		}
	} else {
		if err := setupCPUSets(); err != nil {
			glog.Errorf("Can't set cpusets for emulator: %v", err)
			os.Exit(1)
		}
		// this log hides errors returned by libvirt virError
		// because of libvirt's output parsing approach
		// glog.V(0).Infof("Executing emulator: %s", strings.Join(args, " "))
		if err := syscall.Exec(args[0], args, env); err != nil {
			glog.Errorf("Can't exec emulator: %v", err)
			os.Exit(1)
		}
	}
}

func setupCPUSets() error {
	cpusets := os.Getenv(config.CpusetsEnvVarName)
	if cpusets == "" {
		return nil
	}

	controller, err := cgroups.GetProcessController("self", "cpuset")
	if err != nil {
		return err
	}

	return controller.Set("cpus", cpusets)
}
