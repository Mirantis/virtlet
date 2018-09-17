/*
Copyright 2017 Mirantis

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

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("QEMU Process", func() {
	var (
		vm           *framework.VMInterface
		ssh          framework.Executor
		containerId  string
		vmsContainer framework.Executor
	)

	BeforeAll(func() {
		vm = controller.VM("kill-qemu-vm")
		Expect(vm.CreateAndWait(VMOptions{}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())
		var err error
		vmPod, err := vm.Pod()
		Expect(err).NotTo(HaveOccurred())
		containerId = vmPod.Pod.Status.ContainerStatuses[0].ContainerID
		p := strings.LastIndex(containerId, "__")
		if p >= 0 {
			containerId = containerId[p+2:]
		}
		virtletPod, err := vm.VirtletPod()
		Expect(err).NotTo(HaveOccurred())
		// The container doesn't matter much here because Virtlet pod
		// uses host PID namespace, but let's use vms because that's
		// where the VMs live, and we want to look for QEMU processes.
		// This way this will work even if hostPID is turned off for
		// Virtlet pod.
		vmsContainer, err = virtletPod.Container("vms")
		Expect(err).NotTo(HaveOccurred())
	})

	scheduleWaitSSH(&vm, &ssh)

	It("Must be active while VM is running and gone after it's deleted [Conformance]", func() {
		qemuRunning := func() bool {
			_, _, err := framework.Run(vmsContainer, "",
				"pgrep", "-f", fmt.Sprintf("qemu-system-x86_64.* %s", containerId),
			)
			if ce, ok := err.(framework.CommandError); ok {
				return ce.ExitCode == 0
			}
			Expect(err).NotTo(HaveOccurred())
			return true
		}
		Expect(qemuRunning()).To(BeTrue())
		deleteVM(vm)
		Expect(qemuRunning()).To(BeFalse())
	})
})
