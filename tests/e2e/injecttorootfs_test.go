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

func describeInjectingFiles(what, source, podName string, createSource, delSource func() error) {
	Context("VM with files modified by content from a "+what, func() {
		var vm *framework.VMInterface

		BeforeAll(func() {
			Expect(createSource()).To(Succeed())

			vm = controller.VM(podName)
			Expect(vm.CreateAndWait(VMOptions{
				InjectFilesToRootfsFrom: source,
			}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			Expect(delSource()).To(Succeed())
		})

		It("should put files onto the rootfs [Conformance]", func() {
			ssh := waitSSH(vm)
			Expect(framework.RunSimple(ssh, "cat", "/tmp/test-file1", "/tmp/test-file2")).To(Equal("Hello World!"))
		})
	})
}

var _ = Describe("Injecting files into rootfs", func() {
	testData := map[string]string{
		// plain encoding
		"test_file1":          "Hello ",
		"test_file1_path":     "/tmp/test-file1",
		"test_file1_encoding": "plain",
		// base64 encoding - encoded string: "World!"
		"test_file2":      "V29ybGQh",
		"test_file2_path": "/tmp/test-file2",
	}

	describeInjectingFiles("ConfigMap", "configmap/files", "rootfs-cm", func() error {
		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "files",
			},
			Data: testData,
		}
		_, err := controller.ConfigMaps().Create(cm)
		return err
	}, func() error {
		return controller.ConfigMaps().Delete("files", nil)
	})

	describeInjectingFiles("Secret", "secret/files", "rootfs-secret", func() error {
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "files",
			},
			StringData: testData,
		}
		_, err := controller.Secrets().Create(secret)
		return err
	}, func() error {
		return controller.Secrets().Delete("files", nil)
	})
})
