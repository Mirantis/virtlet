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
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Virtlet restart", func() {
	var (
		vm    *framework.VMInterface
		vmPod *framework.PodInterface
	)

	BeforeAll(func() {
		vm = controller.VM("cirros-vm")
		vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, nil)
		var err error
		vmPod, err = vm.Pod()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		deleteVM(vm)
	})

	It("Should allow to ssh to VM after virtlet pod and vm restart [Conformance]", func() {
		pod, err := vm.VirtletPod()
		Expect(err).NotTo(HaveOccurred())

		virtletPodExecutor, err := pod.Container("virtlet")
		Expect(err).NotTo(HaveOccurred())

		do(framework.ExecSimple(virtletPodExecutor, "pkill", "-9", "virtlet"))
		Expect(pod.Wait()).NotTo(HaveOccurred())

		_, err = vm.VirshCommand("reboot", "<domain>")
		Expect(err).NotTo(HaveOccurred())

		waitSSH(vm)
	}, 3*60)
})
