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
		// Create a VM, wait for it to start and delete it, so we can check
		// if network namespace was deleted
		It("Should delete network namespace when VM is deleted", func() {
			vm = controller.VM("cirros-vm")
			err := vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, nil)
			Expect(err).NotTo(HaveOccurred())

			virtletPod, err := vm.VirtletPod()
			Expect(err).NotTo(HaveOccurred())
			container, err := virtletPod.Container("virtlet")
			Expect(err).NotTo(HaveOccurred())

			pod, err := vm.Pod()
			Expect(err).NotTo(HaveOccurred())

			stdout, err := getNamespaces(container)
			Expect(stdout).To(ContainSubstring(string(pod.Pod.UID)))

			waitSSH(vm)
			deleteVM(vm)

			stdout, err = getNamespaces(container)
			Expect(stdout).NotTo(ContainSubstring(string(pod.Pod.UID)))
		}, 3*60)
	})
})

func getNamespaces(container framework.Executor) (string, error) {
	cmd := []string{"ip", "netns"}
	return framework.RunSimple(container, cmd...)
}
