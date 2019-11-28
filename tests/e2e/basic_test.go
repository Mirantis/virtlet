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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/pkg/tools"
	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Virtlet [Basic tests]", func() {
	var (
		vm    *framework.VMInterface
		vmPod *framework.PodInterface
	)

	BeforeAll(func() {
		vm = controller.VM("test-vm")
		Expect(vm.CreateAndWait(VMOptions{}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())
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

		itShouldHaveNetworkConnectivity(
			func() *framework.PodInterface { return vmPod },
			func() framework.Executor { return ssh },
			true)

		It("Should have hostname equal to the pod name [Conformance]", func() {
			Expect(framework.RunSimple(ssh, "hostname")).To(Equal(vmPod.Pod.Name))
		})

		It("Should have CPU count that was specified for the pod [Conformance]", func() {
			checkCPUCount(vm, ssh, 1)
		})
	})

	Context("Virtlet logs", func() {
		var (
			logPath      string
			nodeExecutor framework.Executor
			ssh          framework.Executor
		)

		BeforeAll(func() {
			virtletPod, err := vm.VirtletPod()
			Expect(err).NotTo(HaveOccurred())
			nodeExecutor, err = virtletPod.DinDNodeExecutor()
			Expect(err).NotTo(HaveOccurred())

			domain, err := vm.Domain()
			Expect(err).NotTo(HaveOccurred())
			for _, env := range domain.QEMUCommandline.Envs {
				if env.Name == "VIRTLET_CONTAINER_LOG_PATH" {
					logPath = env.Value
				}
			}
			Expect(logPath).NotTo(BeEmpty())
			ssh = waitSSH(vm)
			_, err = framework.RunSimple(ssh, "echo ++foo++ | sudo tee /dev/console")
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should contain the echoed string and each line of the log must be a valid JSON", func() {
			Eventually(func() (string, error) {
				out, err := framework.RunSimple(nodeExecutor, "cat", logPath)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, line := range strings.Split(out, "\n") {
					var entry map[string]string
					if err := json.Unmarshal([]byte(line), &entry); err != nil {
						return "", fmt.Errorf("error unmarshalling json: %v", err)
					}
					b.WriteString(line + "\n")
				}
				return b.String(), nil
			}, 120, 5).Should(ContainSubstring("++foo++"))
		})

		It("Should be readable via Kubernetes API", func() {
			c, err := vmPod.Container("")
			Expect(err).NotTo(HaveOccurred())
			Eventually(c.Logs, 120, 5).Should(ContainSubstring("++foo++"))
		})
	})

	It("Should provide VNC interface [Conformance]", func(done Done) {
		defer close(done)
		pod, err := vm.VirtletPod()
		Expect(err).NotTo(HaveOccurred())

		virtletPodExecutor, err := pod.Container("virtlet")
		Expect(err).NotTo(HaveOccurred())

		display, err := vm.VirshCommand("vncdisplay", "<domain>")
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("Taking VNC display snapshot from %s", display))
		do(framework.RunSimple(virtletPodExecutor, "vncsnapshot", "-allowblank", display, "/vm.jpg"))
	}, 60)

	It("Should support port forwarding", func(done Done) {
		defer close(done)
		podName := "nginx-pf"

		By(fmt.Sprintf("Starting nginx pod"))
		nginxPod, err := controller.RunPod(
			podName, framework.NginxImage,
			framework.RunPodOptions{
				ExposePorts: []int32{80},
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(nginxPod).NotTo(BeNil())

		ports := []*tools.ForwardedPort{
			{RemotePort: 80},
		}
		fwStopCh, err := nginxPod.PortForward(ports)
		Expect(err).NotTo(HaveOccurred())
		defer close(fwStopCh)

		localPort := ports[0].LocalPort
		By(fmt.Sprintf("Checking if nginx is available via http://localhost:%d", localPort))
		Eventually(func() (string, error) {
			return framework.Curl(fmt.Sprintf("http://localhost:%d", localPort))
		}, 60).Should(ContainSubstring("nginx web server"))

		Expect(nginxPod.Delete()).To(Succeed())
	}, 60)
})

var _ = Describe("Virtlet [Disruptive]", func() {
	var (
		vm *framework.VMInterface
	)

	BeforeAll(func() {
	})

	AfterAll(func() {
		if vm != nil {
			deleteVM(vm)
		}
	})

	It("Should survive restarting libvirt container", func() {
		virtletNodeName, err := controller.VirtletNodeName()
		Expect(err).NotTo(HaveOccurred())
		nodeExecutor, err := controller.DinDNodeExecutor(virtletNodeName)
		Expect(err).NotTo(HaveOccurred())
		_, err = framework.RunSimple(nodeExecutor, "pkill", "libvirtd")
		Expect(err).NotTo(HaveOccurred())

		vm = controller.VM("cirros-vm")
		Expect(vm.CreateAndWait(VMOptions{}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())
	})
})

func itShouldHaveNetworkConnectivity(podIface func() *framework.PodInterface, ssh func() framework.Executor, conformance bool) {
	suffix := ""
	if conformance {
		suffix = " [Conformance]"
	}

	It("Should have default route"+suffix, func() {
		Expect(framework.RunSimple(ssh(), "/sbin/ip r")).To(ContainSubstring("default via"))
	})

	It("Should have internet connectivity"+suffix, func(done Done) {
		defer close(done)
		Expect(framework.RunSimple(ssh(), "ping -c1 8.8.8.8")).To(MatchRegexp(
			"1 .*transmitted, 1 .*received, 0% .*loss"))
	}, 5)

	Context("With nginx server", func() {
		var nginxPod *framework.PodInterface

		BeforeAll(func() {
			p, err := controller.RunPod(
				"nginx", framework.NginxImage,
				framework.RunPodOptions{
					ExposePorts: []int32{80},
				})
			Expect(err).NotTo(HaveOccurred())
			Expect(p).NotTo(BeNil())
			nginxPod = p
		})

		AfterAll(func() {
			Expect(nginxPod.Delete()).To(Succeed())
		})

		It("Should be able to access another k8s endpoint"+suffix, func(done Done) {
			defer close(done)
			// wget is present in CirrOS, Ubuntu and Debian images that we use, unlike curl,
			// so we use it here
			cmd := fmt.Sprintf("wget -O - -T 5 http://nginx.%s.svc.cluster.local", controller.Namespace())
			Eventually(func() (string, error) {
				return framework.RunSimple(ssh(), cmd)
			}, 60).Should(ContainSubstring("Thank you for using nginx."))
		}, 60*5)
	})
}

var _ = Describe("Virtlet [Image verification]", func() {
	var (
		vm *framework.VMInterface
	)

	BeforeAll(func() {
		vm = controller.VM("cirros-vm")
	})

	AfterAll(func() {
		Expect(vm.Delete(time.Minute * 2)).To(Succeed())
	})

	It("Should fail for images with mismatching digest", func() {
		opts := VMOptions{}.ApplyDefaults()
		p := strings.Index(opts.Image, "@")
		if p >= 0 {
			opts.Image = opts.Image[:p]
		}
		opts.Image += "@sha256:0000000000000000000000000000000000000000000000000000000000000000"
		Expect(vm.Create(opts, nil)).To(Succeed())

		pod := vm.PodWithoutChecks()
		Expect(pod.WaitForPodStatus([]string{
			"ErrImagePull", "ImagePullBackOff", "ImageInspectError",
		}, time.Minute*5)).To(Succeed())
		events, err := pod.LoadEvents()
		Expect(err).NotTo(HaveOccurred())
		Expect(events).To(ContainElement(ContainSubstring("image digest mismatch")))
	})
})
