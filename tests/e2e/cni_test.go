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

var _ = Describe("Virtlet CNI", func() {
	var (
		vm *framework.VMInterface
	)

	Context("Teardown", func() {
		It("Should delete network namespace when VM is deleted", func() {
			// Create a VM, wait for it to start and delete it, so we can check
			// if network namespace was deleted
			vm = controller.VM("cirros-vm")
			vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, nil)
			var err error
			Expect(err).NotTo(HaveOccurred())
			waitSSH(vm)
			deleteVM(vm)

			virtletPod, err := vm.VirtletPod()
			Expect(err).NotTo(HaveOccurred())

			container, err := virtletPod.Container("virtlet")
			Expect(err).NotTo(HaveOccurred())

			cmd := []string{"ip", "netns"}
			stdout, err := framework.RunSimple(container, cmd...)
			Expect(err).NotTo(HaveOccurred())
			pod, err := vm.Pod()
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout).NotTo(ContainSubstring(string(pod.Pod.UID)))

		}, 3*60)
	})
})
