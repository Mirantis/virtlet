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

var _ = Describe("Cloud-init related tests", func() {
	Context("VM with SSH key from implicit key of ConfigMap", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cm-ssh-key-impl",
				},
				Data: map[string]string{
					"authorized_keys": sshPublicKey,
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("ssh-from-cm-impl")
			Expect(vm.Create(VMOptions{
				SSHKeySource: "configmap/cm-ssh-key-impl",
			}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("cm-ssh-key-impl", nil)
		})

		It("Should have SSH accessible [Conformance]", func() {
			waitSSH(vm)
		})
	})

	Context("VM with SSH key from explicit key of ConfigMap", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cm-ssh-key-expl",
				},
				Data: map[string]string{
					"myKey": sshPublicKey,
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("ssh-from-cm-expl")
			Expect(vm.Create(VMOptions{
				SSHKeySource: "configmap/cm-ssh-key-expl/myKey",
			}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("cm-ssh-key-expl", nil)
		})

		It("Should have SSH accessible [Conformance]", func() {
			waitSSH(vm)
		})
	})

	Context("VM with SSH key from implicit key of Secret", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secret-ssh-key-impl",
				},
				StringData: map[string]string{
					"authorized_keys": sshPublicKey,
				},
			}
			_, err := controller.Secrets().Create(secret)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("ssh-from-secret-impl")
			Expect(vm.Create(VMOptions{
				SSHKeySource: "secret/secret-ssh-key-impl",
			}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.Secrets().Delete("secret-ssh-key-impl", nil)
		})

		It("Should have SSH accessible [Conformance]", func() {
			waitSSH(vm)
		})
	})

	Context("VM with SSH key from explicit key of Secret", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secret-ssh-key-expl",
				},
				StringData: map[string]string{
					"myKey": sshPublicKey,
				},
			}
			_, err := controller.Secrets().Create(secret)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("ssh-from-secret-expl")
			Expect(vm.Create(VMOptions{
				SSHKeySource: "secret/secret-ssh-key-expl/myKey",
			}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.Secrets().Delete("secret-ssh-key-expl", nil)
		})

		It("Should have SSH accessible [Conformance]", func() {
			waitSSH(vm)
		})
	})

	Context("User-data from ConfigMap", func() {
		var vm *framework.VMInterface

		const fileConf = `[{"content": "Hello world!", "path": "/tmp/test-file"}]`

		BeforeAll(func() {
			requireCloudInit()
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cm-userdata",
				},
				Data: map[string]string{
					"write_files": fileConf,
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("userdata-cm")
			Expect(vm.Create(VMOptions{
				UserDataSource: "configmap/cm-userdata",
			}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("cm-userdata", nil)
		})

		It("Must be processed [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/test-file")).To(Equal("Hello world!"))
		})
	})

	Context("User-data from Secret", func() {
		var vm *framework.VMInterface

		const fileConf = `[{"content": "Hello world!", "path": "/tmp/test-file"}]`

		BeforeAll(func() {
			requireCloudInit()
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secret-userdata",
				},
				StringData: map[string]string{
					"write_files": fileConf,
				},
			}
			_, err := controller.Secrets().Create(secret)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("userdata-secret")
			Expect(vm.Create(VMOptions{
				UserDataSource: "secret/secret-userdata",
			}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.Secrets().Delete("secret-userdata", nil)
		})

		It("Must be processed [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/test-file")).To(Equal("Hello world!"))
		})
	})

	Context("User-data merged from two sources", func() {
		var vm *framework.VMInterface

		const fileConf = `[{"content": "Hello ", "path": "/tmp/test-file1"}]`
		const userData = `{"write_files": [{"content": "world!", "path": "/tmp/test-file2"}]}`

		BeforeAll(func() {
			requireCloudInit()
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cm-userdata",
				},
				Data: map[string]string{
					"write_files": fileConf,
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("userdata-cm-merge")
			Expect(vm.Create(VMOptions{
				UserDataSource: "configmap/cm-userdata",
				UserData:       userData,
			}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("cm-userdata", nil)
		})

		It("Must be processed [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/test-file1", "/tmp/test-file2")).To(Equal("Hello world!"))
		})
	})

})
