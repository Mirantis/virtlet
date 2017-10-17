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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Container volume mounts", func() {
	Context("of raw volumes", func() {
		var (
			vm           *framework.VMInterface
			nodeExecutor framework.Executor
			devPath      string
			ssh          framework.Executor
		)

		BeforeAll(func() {
			requireCloudInit()
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

		scheduleWaitSSH(&vm, &ssh)
		itShouldBeMounted(&ssh)
	})

	Context("of ephemeral volumes", func() {
		var (
			vm  *framework.VMInterface
			ssh framework.Executor
		)

		BeforeAll(func() {
			requireCloudInit()
			vm = makeVolumeMountVM(map[string]string{
				"type":     "qcow2",
				"capacity": "10MB",
			}, "")
		})

		AfterAll(func() {
			deleteVM(vm)
		})

		scheduleWaitSSH(&vm, &ssh)
		itShouldBeMounted(&ssh)
	})

	Context("ConfigMap files must be mounted", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			requireCloudInit()
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "files-cm",
				},
				Data: map[string]string{
					"file1": "Hello ",
					"file2": "world!",
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("cm-files")
			podCustomization := func(pod *framework.PodInterface) {
				pod.Pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
					{
						Name:      "vol",
						MountPath: "/tmp/vol",
					},
				}
				pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
					Name: "vol",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "files-cm",
							},
							Items: []v1.KeyToPath{
								{
									Path: "test-file1",
									Key:  "file1",
								},
								{
									Path: "test-file2",
									Key:  "file2",
								},
							},
						},
					},
				})
			}

			vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, podCustomization)
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("files-cm", nil)
		})

		It("And files must be found on VM", func() {
			ssh := waitSSH(vm)
			Expect(framework.ExecSimple(ssh, "cat", "/tmp/vol/test-file1", "/tmp/vol/test-file2")).To(Equal("Hello world!"))
		})
	})

	Context("Secret files must be mounted", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			requireCloudInit()
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "files-secret",
				},
				StringData: map[string]string{
					"file1": "Hello ",
					"file2": "world!",
				},
			}
			_, err := controller.Secrets().Create(secret)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("secret-files")
			podCustomization := func(pod *framework.PodInterface) {
				pod.Pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
					{
						Name:      "vol",
						MountPath: "/tmp/vol",
					},
				}
				pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
					Name: "vol",
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							SecretName: "files-secret",
							Items: []v1.KeyToPath{
								{
									Path: "test-file1",
									Key:  "file1",
								},
								{
									Path: "test-file2",
									Key:  "file2",
								},
							},
						},
					},
				})
			}

			vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, podCustomization)
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.Secrets().Delete("files-secret", nil)
		})

		It("And files must be found on VM", func() {
			ssh := waitSSH(vm)
			Expect(framework.ExecSimple(ssh, "cat", "/tmp/vol/test-file1", "/tmp/vol/test-file2")).To(Equal("Hello world!"))
		})
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
