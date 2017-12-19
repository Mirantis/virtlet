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

	"github.com/Mirantis/virtlet/pkg/nettools"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
)

// Here we use cgo constructor trick to avoid threading-related problems
// (not being able to enter the mount namespace)
// when working with process uids/gids and namespaces
// https://github.com/golang/go/issues/8676#issuecomment-66098496

/*
#define _GNU_SOURCE

#include <stdlib.h>
#include <stdio.h>
#include <fcntl.h>
#include <sched.h>
#include <unistd.h>
#include <sys/mount.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <linux/limits.h>

static void vmwrapper_perr(const char* msg) {
	perror(msg);
	exit(1);
}

static void vmwrapper_setns(int my_pid, int target_pid, int nstype, const char* nsname) {
	int my_ns_inode, fd;
        struct stat st;
	char my_ns_path[PATH_MAX], target_ns_path[PATH_MAX];
	snprintf(my_ns_path, sizeof(my_ns_path), "/proc/%u/ns/%s", my_pid, nsname);
	snprintf(target_ns_path, sizeof(target_ns_path), "/proc/%u/ns/%s", target_pid, nsname);
	if (stat(my_ns_path, &st) < 0) {
		vmwrapper_perr("stat() my ns");
	}
	my_ns_inode = st.st_ino;
	if (stat(target_ns_path, &st) < 0) {
		vmwrapper_perr("stat() target ns");
	}

	// Check if that's the same namespace
	// (actually only critical for CLONE_NEWUSER)
	if (my_ns_inode == st.st_ino)
		return;

	if ((fd = open(target_ns_path, O_RDONLY)) < 0) {
		vmwrapper_perr("open() target ns");
	}

	if (setns(fd, nstype) < 0) {
		vmwrapper_perr("setns()");
	}
}

// This function is a high-priority constructor that will be invoked
// before any Go code starts.
__attribute__((constructor (200))) void vmwrapper_handle_reexec(void) {
	int my_pid, target_pid;
	char* pid_str;
	if ((pid_str = getenv("VMWRAPPER_NS_PID")) == NULL)
		return;

	my_pid = getpid();
        target_pid = atoi(pid_str);

        // Other namespaces:
        // cgroup, user - not touching
        // pid - host pid namespace is used by virtlet
        // net - host network is used by virtlet
	fprintf(stderr, "vmwrapper reexec: entering vms container namespaces\n");
	vmwrapper_setns(my_pid, target_pid, CLONE_NEWNS, "mnt");
	vmwrapper_setns(my_pid, target_pid, CLONE_NEWUTS, "uts");
	vmwrapper_setns(my_pid, target_pid, CLONE_NEWIPC, "ipc");

	// remount /sys for new netns
	if (umount2("/sys", MNT_DETACH) < 0)
		vmwrapper_perr("umount2()");
	if (mount("none", "/sys", "sysfs", 0, NULL) < 0)
		vmwrapper_perr("mount()");

	// permanently drop privs if SR-IOV support is not enabled
	if (getenv("VMWRAPPER_KEEP_PRIVS") == NULL) {
		fprintf(stderr, "vmwrapper reexec: dropping privs\n");
		if (setgid(getgid()) < 0)
			vmwrapper_perr("setgid()");
		if (setuid(getuid()) < 0)
			vmwrapper_perr("setuid()");
	} else {
		if (setgid(0) < 0)
			vmwrapper_perr("setgid()");
		if (setuid(0) < 0)
			vmwrapper_perr("setuid()");
	}
}
*/
import "C"

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

func main() {
	// configure glog (apparently no better way to do it ...)
	flag.CommandLine.Parse([]string{"-v=3", "-alsologtostderr=true"})

	if os.Getenv("VMWRAPPER_NS_PID") != "" {
		if err := syscall.Exec(os.Args[1], os.Args[1:], os.Environ()); err != nil {
			glog.Errorf("Can't exec emulator: %v", err)
			os.Exit(1)
		}
	}

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
				case nettools.InterfaceTypeTap:
					netArgs = append(netArgs,
						"-netdev",
						fmt.Sprintf("tap,id=tap%d,fd=%d", desc.FdIndex, fds[desc.FdIndex]),
						"-device",
						fmt.Sprintf("virtio-net-pci,netdev=tap%d,id=net%d,mac=%s", desc.FdIndex, i, desc.HardwareAddr),
					)
				case nettools.InterfaceTypeVF:
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
		// re-execute itself because entering mount namespace
		// is impossible after Go runtime spawns some threads
		env = append(env, fmt.Sprintf("VMWRAPPER_NS_PID=%d", pid))
		args = append([]string{os.Args[0]}, args...)
	}

	// below log hides any possible error in returned by libvirt virError
	// glog.V(0).Infof("Executing emulator: %s", strings.Join(args, " "))
	if err := syscall.Exec(args[0], args, env); err != nil {
		glog.Errorf("Can't exec emulator: %v", err)
		os.Exit(1)
	}
}
