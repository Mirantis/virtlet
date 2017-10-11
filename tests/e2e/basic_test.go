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
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Basic cirros tests", func() {
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

	Context("VM guest OS", func() {
		var ssh framework.Executor
		scheduleWaitSSH(&vm, &ssh)

		It("Should have default route", func() {
			Expect(framework.ExecSimple(ssh, "ip r")).To(SatisfyAll(
				ContainSubstring("default via"),
				ContainSubstring("src "+vmPod.Pod.Status.PodIP),
			))
		})

		It("Should have internet connectivity", func(done Done) {
			defer close(done)
			Expect(framework.ExecSimple(ssh, "ping -c1 8.8.8.8")).To(ContainSubstring(
				"1 packets transmitted, 1 packets received, 0% packet loss"))
		}, 5)

		Context("With nginx server", func() {
			var nginxPod *framework.PodInterface

			BeforeAll(func() {
				p, err := controller.RunPod("nginx", "nginx", nil, time.Minute*4, 80)
				Expect(err).NotTo(HaveOccurred())
				Expect(p).NotTo(BeNil())
				nginxPod = p
			})

			AfterAll(func() {
				Expect(nginxPod.Delete()).To(Succeed())
			})

			It("Should be able to access another k8s endpoint", func(done Done) {
				defer close(done)
				cmd := fmt.Sprintf("curl -s --connect-timeout 5 http://nginx.%s.svc.cluster.local", controller.Namespace())
				Eventually(func() (string, error) {
					return framework.ExecSimple(ssh, cmd)
				}, 60).Should(ContainSubstring("Thank you for using nginx."))
			}, 60*5)
		})

		It("Should have hostname equal to the pod name", func() {
			Expect(framework.ExecSimple(ssh, "hostname")).To(Equal(vmPod.Pod.Name))
		})

		It("Should have CPU count that was specified for the pod", func() {
			checkCPUCount(vm, ssh, 1)
		})
	})

	Context("Virtlet logs", func() {
		var (
			filename     string
			sandboxID    string
			nodeExecutor framework.Executor
		)

		BeforeAll(func() {
			virtletPod, err := vm.VirtletPod()
			Expect(err).NotTo(HaveOccurred())
			nodeExecutor, err = virtletPod.DinDNodeExecutor()
			Expect(err).NotTo(HaveOccurred())

			domain, err := vm.Domain()
			Expect(err).NotTo(HaveOccurred())
			var vmName, attempt string
			for _, env := range domain.QEMUCommandline.Envs {
				if env.Name == "VIRTLET_POD_NAME" {
					vmName = env.Value
				} else if env.Name == "CONTAINER_ATTEMPTS" {
					attempt = env.Value
				} else if env.Name == "VIRTLET_POD_UID" {
					sandboxID = env.Value
				}
			}
			Expect(sandboxID).NotTo(BeEmpty())
			Expect(vmName).NotTo(BeEmpty())
			Expect(attempt).NotTo(BeEmpty())
			filename = fmt.Sprintf("%s_%s.log", vmName, attempt)
		})

		It("Should contain login string in pod log and each line of that log must be a valid JSON", func() {
			Eventually(func() error {
				out, err := framework.ExecSimple(nodeExecutor, "cat",
					fmt.Sprintf("/var/log/pods/%s/%s", sandboxID, filename))
				if err != nil {
					return err
				}
				found := 0
				for _, line := range strings.Split(out, "\n") {
					var entry map[string]string
					if err := json.Unmarshal([]byte(line), &entry); err != nil {
						return fmt.Errorf("error unmarshalling json: %v", err)
					}
					if strings.HasPrefix(entry["log"], "login as 'cirros' user. default password") {
						found++
					}
				}
				if found != 1 {
					return fmt.Errorf("expected login prompt to appear exactly once in the log, but got %d occurences", found)
				}
				return nil
			})
		})
	})

	It("Should provide VNC interface", func(done Done) {
		defer close(done)
		pod, err := vm.VirtletPod()
		Expect(err).NotTo(HaveOccurred())

		virtletPodExecutor, err := pod.Container("virtlet")
		Expect(err).NotTo(HaveOccurred())

		display, err := vm.VirshCommand("vncdisplay", "<domain>")
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("Taking VNC display snapshot from %s", display))
		do(framework.ExecSimple(virtletPodExecutor, "vncsnapshot", "-allowblank", display, "/vm.jpg"))
	}, 60)

	It("Should start port forwarding", func(done Done) {
		defer close(done)
		podName := "nginx-pf"
		localPort := rand.Intn(899) + 100
		portMapping := fmt.Sprintf("18%d:80", localPort)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By(fmt.Sprintf("Starting nginx pod"))
		nginxPod, err := controller.RunPod(podName, "nginx", nil, time.Minute*4, 80)
		Expect(err).NotTo(HaveOccurred())
		Expect(nginxPod).NotTo(BeNil())

		By(fmt.Sprintf("Running command: kubectl -n %s port-forward %s %s", controller.Namespace(), podName, portMapping))
		_, err = framework.ExecSimple(localExecutor, "kubectl", "-n", controller.Namespace(), "port-forward", podName, portMapping)
		Expect(err).NotTo(HaveOccurred())
		// give it a chance to start
		time.Sleep(3 * time.Second)

		By(fmt.Sprintf("Checking if nginx is available via localhost"))
		data, err := framework.Curl(fmt.Sprintf("http://localhost:18%d", localPort))
		Expect(err).NotTo(HaveOccurred())
		Expect(data).Should(ContainSubstring("nginx web server"))

		Expect(nginxPod.Delete()).To(Succeed())
	}, 60)
})
