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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Container volume mounts", func() {
	Context("Of raw volumes", func() {
		var (
			err             error
			virtletNodeName string
			vm              *framework.VMInterface
			nodeExecutor    framework.Executor
			devPath         string
			ssh             framework.Executor
		)

		BeforeAll(func() {
			virtletNodeName, err = controller.VirtletNodeName()
			Expect(err).NotTo(HaveOccurred())
			nodeExecutor, err = controller.DinDNodeExecutor(virtletNodeName)
			Expect(err).NotTo(HaveOccurred())

			_, err = framework.RunSimple(nodeExecutor, "dd", "if=/dev/zero", "of=/rawdevtest", "bs=1M", "count=10")
			Expect(err).NotTo(HaveOccurred())
			_, err = framework.RunSimple(nodeExecutor, "mkfs.ext4", "/rawdevtest")
			Expect(err).NotTo(HaveOccurred())
			devPath, err = framework.RunSimple(nodeExecutor, "losetup", "-f", "/rawdevtest", "--show")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if ssh != nil {
				ssh.Close()
			}
			if vm != nil {
				deleteVM(vm)
			}
		})

		AfterAll(func() {
			// The loopback device is detached by itself upon
			// success (TODO: check why it happens), so we
			// ignore errors here
			framework.RunSimple(nodeExecutor, "losetup", "-d", devPath)
		})

		It("Should be handled inside the VM", func() {
			vm = makeVolumeMountVM(virtletNodeName, func(pod *framework.PodInterface) {
				addFlexvolMount(pod, "blockdev", "/foo", map[string]string{
					"type": "raw",
					"path": devPath,
					"part": "0",
				})
			})
			ssh = waitSSH(vm)
			shouldBeMounted(ssh, "/foo")
		})

		It("Should be handled inside the VM together with another volume mount", func() {
			vm = makeVolumeMountVM(virtletNodeName, func(pod *framework.PodInterface) {
				addFlexvolMount(pod, "blockdev1", "/foo", map[string]string{
					"type": "raw",
					"path": devPath,
					"part": "0",
				})
				addFlexvolMount(pod, "blockdev2", "/bar", map[string]string{
					"type":     "qcow2",
					"capacity": "10MB",
				})
			})
			ssh = waitSSH(vm)
			shouldBeMounted(ssh, "/foo")
			shouldBeMounted(ssh, "/bar")
		})
	})

	Context("Of ephemeral volumes", func() {
		var (
			vm  *framework.VMInterface
			ssh framework.Executor
		)

		BeforeAll(func() {
			vm = makeVolumeMountVM("", func(pod *framework.PodInterface) {
				addFlexvolMount(pod, "blockdev", "/foo", map[string]string{
					"type":     "qcow2",
					"capacity": "10MB",
				})
			})
		})

		AfterAll(func() {
			deleteVM(vm)
		})

		scheduleWaitSSH(&vm, &ssh)
		It("Should be handled inside the VM [Conformance]", func() {
			shouldBeMounted(ssh, "/foo")
		})
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

			Expect(vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, podCustomization)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("files-cm", nil)
		})

		It("And files must be found on VM [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/vol/test-file1", "/tmp/vol/test-file2")).To(Equal("Hello world!"))
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

			Expect(vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, podCustomization)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.Secrets().Delete("files-secret", nil)
		})

		It("And files must be found on VM [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/vol/test-file1", "/tmp/vol/test-file2")).To(Equal("Hello world!"))
		})
	})

})

func addFlexvolMount(pod *framework.PodInterface, name string, mountPath string, flexvolOptions map[string]string) {
	pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
		Name: name,
		VolumeSource: v1.VolumeSource{
			FlexVolume: &v1.FlexVolumeSource{
				Driver:  "virtlet/flexvolume_driver",
				Options: flexvolOptions,
			},
		},
	})
	pod.Pod.Spec.Containers[0].VolumeMounts = append(pod.Pod.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
		Name:      name,
		MountPath: mountPath,
	})
}

func makeVolumeMountVM(nodeName string, podCustomization func(*framework.PodInterface)) *framework.VMInterface {
	vm := controller.VM("mount-vm")
	Expect(vm.Create(VMOptions{
		NodeName: nodeName,
		// TODO: should also have an option to test using
		// ubuntu image with volumes mounted using cloud-init
		// userdata 'mounts' section
		UserDataScript: "@virtlet-mount-script@",
	}.applyDefaults(), time.Minute*5, podCustomization)).To(Succeed())
	_, err := vm.Pod()
	Expect(err).NotTo(HaveOccurred())
	return vm
}

func shouldBeMounted(ssh framework.Executor, path string) {
	Eventually(func() (string, error) {
		return framework.RunSimple(ssh, "ls -l "+path)
	}, 60).Should(ContainSubstring("lost+found"))
}
