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
	"bytes"
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Virtlet restart [Disruptive]", func() {
	var (
		vm    *framework.VMInterface
		vmPod *framework.PodInterface
	)

	BeforeAll(func() {
		vm = controller.VM("cirros-vm")
		vm.Create(VMOptions{}.ApplyDefaults(), time.Minute*5, nil)
		var err error
		vmPod, err = vm.Pod()
		Expect(err).NotTo(HaveOccurred())

		// restart virtlet before all tests
		virtletPod, err := vm.VirtletPod()
		Expect(err).NotTo(HaveOccurred())

		err = virtletPod.Delete()
		Expect(err).NotTo(HaveOccurred())

		waitVirtletPod(vm)
	})

	AfterAll(func() {
		deleteVM(vm)
	})

	It("Should allow to ssh to VM after virtlet pod restart", func() {
		waitSSH(vm)
	}, 3*60)

	It("Should keep logs from another session", func() {
		var stdout bytes.Buffer
		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By(fmt.Sprintf("Running command: kubectl logs -n %s %s", controller.Namespace(), vm.Name))
		err := localExecutor.Run(nil, &stdout, &stdout, "kubectl", "-n", controller.Namespace(), "logs", vm.Name)
		fmt.Sprintf(stdout.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(stdout.String()).Should(ContainSubstring("login as 'cirros' user."))

		By(fmt.Sprintf("Running command: kubectl attach -n %s -i %s", controller.Namespace(), vm.Name))
		stdin := bytes.NewBufferString("\nTESTTEXT\n\n")
		stdout.Reset()
		err = localExecutor.Run(stdin, &stdout, &stdout, "kubectl", "-n", controller.Namespace(), "attach", "-i", vm.Name)
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("Running again command: kubectl logs -n %s %s", controller.Namespace(), vm.Name))
		stdout.Reset()
		err = localExecutor.Run(nil, &stdout, &stdout, "kubectl", "-n", controller.Namespace(), "logs", vm.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(stdout.String()).Should(ContainSubstring("TESTTEXT"))
	}, 3*60)
})
