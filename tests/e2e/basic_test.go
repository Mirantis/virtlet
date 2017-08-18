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
	"time"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
	. "github.com/onsi/gomega"
)

var _ = Describe("Basic cirros tests", func() {
	var err error
	var controller *framework.Controller
	var vm *framework.VMInterface
	var vmPod *framework.PodInterface

	BeforeAll(func() {
		controller, err = framework.NewController("")
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("Using namespace %s", controller.Namespace.Name))

		vm = controller.VM("cirros-vm")
		vm.Create(framework.VMOptions{
			Image:           cirrosImage,
			SSHKey:          sshPublicKey,
			VCPUCount:       1,
			CloudInitScript: cloudInitScript,
			DiskDriver:      "virtio",
			Limits: map[string]string{
				"memory": "128Mi",
			},
		}, time.Minute*5)
		vmPod, err = vm.Pod()
		Expect(err).ToNot(HaveOccurred())
	})
	AfterAll(func() {
		controller.Close()
	})

	Context("SSH tests", func() {
		var ssh *framework.SSHInterface

		BeforeAll(func() {
			Eventually(
				func() (string, error) {
					ssh, err = vm.SSH("cirros", sshPrivateKey)
					if err != nil {
						return "", err
					}
					return framework.ExecSimple(ssh)
				}, 60*5, 3).Should(BeEmpty())
		})

		AfterAll(func() {
			ssh.Close()
		})

		It("Check network interface", func(done Done) {
			defer close(done)
			Expect(framework.ExecSimple(ssh, "ip r")).To(And(
				ContainSubstring("default via"),
				ContainSubstring("src "+vmPod.Pod.Status.PodIP),
			))
		})

		It("Check internet connectivity", func(done Done) {
			defer close(done)
			Expect(framework.ExecSimple(ssh, "ping -c1 8.8.8.8")).To(ContainSubstring(
				"1 packets transmitted, 1 packets received, 0% packet loss"))
		}, 5)

		Context("Access another k8s endpoint from VM", func() {
			BeforeAll(func() {
				Expect(controller.RunPod("nginx", "nginx", nil, time.Minute*4, 80)).ToNot(BeNil())
			})

			It("Try to access nginx service by its domain name", func(done Done) {
				defer close(done)
				cmd := fmt.Sprintf("curl -s --connect-timeout 5 http://nginx.%s.svc.cluster.local", controller.Namespace.Name)
				Eventually(func() (string, error) {
					return framework.ExecSimple(ssh, cmd)
				}, 60).Should(ContainSubstring("Thank you for using nginx."))
			}, 60*5)
		})
	})
})
