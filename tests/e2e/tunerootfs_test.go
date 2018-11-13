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
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Rootfs modification releated tests", func() {
	Context("VM with files modified by content of ConfigMap", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cm-files-modification",
				},
				Data: map[string]string{
					// plain encoding
					"test_file1":          "Hello ",
					"test_file1_path":     "/tmp/test-file1",
					"test_file1_encoding": "plain",
					// base64 encoding - encoded string: "World!"
					"test_file2":      "V29ybGQh",
					"test_file2_path": "/tmp/test-file2",
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("files-from-cm")
			Expect(vm.CreateAndWait(VMOptions{
				RootfsFilesSource: "configmap/cm-files-modification",
			}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("cm-files-modification", nil)
		})

		It("Must be processed [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/test-file1", "/tmp/test-file2")).To(Equal("Hello World!"))
		})
	})

	Context("VM with files modified by content of Secret", func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secret-files-modification",
				},
				StringData: map[string]string{
					// plain encoding
					"test_file1":          "Hello ",
					"test_file1_path":     "/tmp/test-file1",
					"test_file1_encoding": "plain",
					// base64 encoding - encoded string: "World!"
					"test_file2":      "V29ybGQh",
					"test_file2_path": "/tmp/test-file2",
				},
			}
			_, err := controller.Secrets().Create(secret)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("files-from-secret")
			Expect(vm.CreateAndWait(VMOptions{
				RootfsFilesSource: "secret/secret-files-modification",
			}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.Secrets().Delete("secret-files-modification", nil)
		})

		It("Must be processed [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/test-file1", "/tmp/test-file2")).To(Equal("Hello World!"))
		})
	})
})
