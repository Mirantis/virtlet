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

var _ = Describe("Hung VM", func() {
	var (
		vm  *framework.VMInterface
		ssh framework.Executor
	)

	BeforeAll(func() {
		vm = controller.VM("hung-vm")
		vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, nil)
		var err error
		_, err = vm.Pod()
		Expect(err).NotTo(HaveOccurred())
	})

	scheduleWaitSSH(&vm, &ssh)

	It("Must be successfully deleted after it hangs [Conformance]", func() {
		Eventually(framework.WithTimeout(time.Second*2, func() error {
			_, err := framework.ExecSimple(ssh, "sudo /sbin/halt -nf")
			return err
		})).Should(MatchError(framework.ErrTimeout))

		deleteVM(vm)
	})
})
