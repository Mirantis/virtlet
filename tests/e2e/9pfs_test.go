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
	"path/filepath"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
	"k8s.io/api/core/v1"
)

const (
	bbName = "busybox-9pfs"
)

var _ = Describe("9pfs volumes [Heavy]", func() {
	var busyboxPod *framework.PodInterface
	var vm *framework.VMInterface
	var ssh framework.Executor

	AfterAll(func() {
		if ssh != nil {
			ssh.Close()
		}
		if vm != nil {
			deleteVM(vm)
		}
		Expect(busyboxPod.Delete()).To(Succeed())
	})

	It("Should work with hostPath volumes", func() {
		if UsingCirros() {
			Skip("9pfs can't be tested using CirrOS at the moment")
		}

		By("Picking a Virtlet node")
		nodeName, err := controller.VirtletNodeName()
		Expect(err).NotTo(HaveOccurred())

		By("Starting a busybox pod")
		busyboxPod, err = controller.RunPod(
			bbName, framework.BusyboxImage,
			framework.RunPodOptions{
				Command:  []string{"/bin/sleep", "1200"},
				NodeName: nodeName,
				HostPathMounts: []framework.HostPathMount{
					{
						HostPath:      "/tmp",
						ContainerPath: "/tmp",
					},
				},
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(busyboxPod).NotTo(BeNil())
		bbExec, err := busyboxPod.Container(bbName)
		Expect(err).NotTo(HaveOccurred())

		By("Creating a temp directory")
		dir, err := framework.RunSimple(bbExec, "/bin/sh", "-c", "d=`mktemp -d`; echo foo >$d/bar; echo $d")
		Expect(err).NotTo(HaveOccurred())
		Expect(dir).NotTo(BeEmpty())

		By("Creating a VM that has temp directory mounted via 9pfs")
		vm = controller.VM("vm-9pfs")
		podCustomization := func(pod *framework.PodInterface) {
			pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
				Name: "9pfs-vol",
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: dir,
					},
				},
			})
			pod.Pod.Spec.Containers[0].VolumeMounts = append(
				pod.Pod.Spec.Containers[0].VolumeMounts,
				v1.VolumeMount{
					Name:      "9pfs-vol",
					MountPath: "/hostmount",
				})
		}
		Expect(vm.CreateAndWait(VMOptions{
			NodeName: nodeName,
		}.ApplyDefaults(), time.Minute*5, podCustomization)).To(Succeed())
		_, err = vm.Pod()
		Expect(err).NotTo(HaveOccurred())

		By("Wait for the volume to be mounted inside the VM")
		ssh = waitSSH(vm)
		Eventually(func() error {
			_, err := framework.RunSimple(ssh, "sudo test -e /hostmount/bar")
			return err
		}, 60*5, 3).Should(Succeed())

		By("Make a copy of a file on the volume inside the VM")
		_, err = framework.RunSimple(ssh, "sudo cp /hostmount/bar /hostmount/bar1")
		Expect(err).NotTo(HaveOccurred())

		By("Verifying the new file contents inside the busybox pod")
		content, err := framework.RunSimple(bbExec, "cat", filepath.Join(dir, "bar1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal("foo"))
	})
})
