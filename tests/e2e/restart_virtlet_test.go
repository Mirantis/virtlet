/*
Copyright 2018 Mirantis

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
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Virtlet restart", func() {
	var (
		vm    *framework.VMInterface
		vmPod *framework.PodInterface
		ssh   framework.Executor
	)

	BeforeAll(func() {
		vm = controller.VM("restart-test-vm")
		vm.CreateAndWait(VMOptions{}.ApplyDefaults(), time.Minute*5, nil)
		var err error
		vmPod, err = vm.Pod()
		Expect(err).NotTo(HaveOccurred())

		preRestartSsh := waitSSH(vm)
		defer preRestartSsh.Close()
		do(framework.RunSimple(preRestartSsh, "echo ++prerestart++ | sudo tee /dev/console"))

		// restart virtlet before all tests
		virtletPod, err := vm.VirtletPod()
		Expect(err).NotTo(HaveOccurred())

		err = virtletPod.Delete()
		Expect(err).NotTo(HaveOccurred())

		waitVirtletPod(vm)
	})

	AfterAll(func() {
		if ssh != nil {
			ssh.Close()
		}
		if vm != nil {
			deleteVM(vm)
		}
	})

	It("Should allow to ssh to VM after virtlet pod restart", func() {
		ssh = waitSSH(vm)
		out := do(framework.RunSimple(ssh, "echo abcdef")).(string)
		Expect(out).To(Equal("abcdef"))
	}, 3*60)

	It("Should keep logs from another session", func() {
		c, err := vmPod.Container("")
		Expect(err).NotTo(HaveOccurred())
		Eventually(c.Logs, 120, 5).Should(ContainSubstring("++prerestart++"))

		ssh = waitSSH(vm)
		do(framework.RunSimple(ssh, "echo ++afterrestart++ | sudo tee /dev/console"))
		Eventually(c.Logs, 60, 5).Should(ContainSubstring("++afterrestart++"))
	}, 3*60)
})
