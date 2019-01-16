/*
Copyright 2019 Mirantis

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

var _ = Describe("SystemUUID passing", func() {
	var (
		vm  *framework.VMInterface
		ssh framework.Executor
	)

	BeforeAll(func() {
		vm = controller.VM("uuid")
		Expect(vm.CreateAndWait(VMOptions{
			SystemUUID: "53008994-44c0-4017-ad44-9c49758083da",
		}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())
		do(vm.Pod())
	})

	AfterAll(func() {
		deleteVM(vm)
	})

	scheduleWaitSSH(&vm, &ssh)

	It("Should have the specified SMBIOS UUID set [Conformance]", func() {
		uuid := do(framework.RunSimple(ssh, "sudo", "cat", "/sys/class/dmi/id/product_uuid")).(string)
		Expect(uuid).To(Equal("53008994-44C0-4017-AD44-9C49758083DA"))
	})
})
