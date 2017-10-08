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
	"k8s.io/client-go/pkg/api/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("[Heavy] Container volume mounts", func() {
	Context("of raw volumes", func() {
		var (
			vm           *framework.VMInterface
			nodeExecutor framework.Executor
			devPath      string
			ssh          framework.Executor
		)

		BeforeAll(func() {
			virtletNodeName, err := controller.VirtletNodeName()
			Expect(err).NotTo(HaveOccurred())
			nodeExecutor, err = controller.DinDNodeExecutor(virtletNodeName)
			Expect(err).NotTo(HaveOccurred())

			_, err = framework.ExecSimple(nodeExecutor, "dd", "if=/dev/zero", "of=/rawdevtest", "bs=1M", "count=10")
			Expect(err).NotTo(HaveOccurred())
			_, err = framework.ExecSimple(nodeExecutor, "mkfs.ext4", "/rawdevtest")
			Expect(err).NotTo(HaveOccurred())
			devPath, err = framework.ExecSimple(nodeExecutor, "losetup", "-f", "/rawdevtest", "--show")
			Expect(err).NotTo(HaveOccurred())

			vm = makeVolumeMountVM(map[string]string{
				"type": "raw",
				"path": devPath,
				"part": "0",
			}, virtletNodeName)
		})

		AfterAll(func() {
			deleteVM(vm)
			// The loopback device is detached by itself upon
			// success (TODO: check why it happens), so we
			// ignore errors here
			framework.ExecSimple(nodeExecutor, "losetup", "-d", devPath)
		})

		scheduleWaitSSH(&vm, &ssh, "ubuntu")
		itShouldBeMounted(&ssh)
	})

	Context("of ephemeral volumes", func() {
		var (
			vm  *framework.VMInterface
			ssh framework.Executor
		)

		BeforeAll(func() {
			vm = makeVolumeMountVM(map[string]string{
				"type":     "qcow2",
				"capacity": "10MB",
			}, "")
		})

		AfterAll(func() {
			deleteVM(vm)
		})

		scheduleWaitSSH(&vm, &ssh, "ubuntu")
		itShouldBeMounted(&ssh)
	})
})

func makeVolumeMountVM(flexvolOptions map[string]string, nodeName string) *framework.VMInterface {
	podCustomization := func(pod *framework.PodInterface) {
		pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
			Name: "blockdev",
			VolumeSource: v1.VolumeSource{
				FlexVolume: &v1.FlexVolumeSource{
					Driver:  "virtlet/flexvolume_driver",
					Options: flexvolOptions,
				},
			},
		})
		pod.Pod.Spec.Containers[0].VolumeMounts = append(pod.Pod.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      "blockdev",
			MountPath: "/foo",
		})
	}
	vm := controller.VM("ubuntu-vm")
	vm.Create(VMOptions{
		Image:    *ubuntuLocation,
		NodeName: nodeName,
	}.applyDefaults(), time.Minute*5, podCustomization)
	_, err := vm.Pod()
	Expect(err).NotTo(HaveOccurred())
	return vm
}

func itShouldBeMounted(ssh *framework.Executor) {
	It("Should be handled inside the VM", func() {
		Eventually(func() (string, error) {
			return framework.ExecSimple(*ssh, "ls -l /foo")
		}, 60).Should(ContainSubstring("lost+found"))
	})
}
