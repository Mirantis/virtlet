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
	"context"
	"io/ioutil"
	"os"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("virtletctl tests", func() {
	It("Should how usage", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl help")
		_, err := framework.ExecSimple(localExecutor, "_output/virtletctl", "help")
		Expect(err).NotTo(HaveOccurred())
	})

	It("Should not fail during dump-metadata", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl dump-metadata")
		_, err := framework.ExecSimple(localExecutor, "_output/virtletctl", "dump-metadata")
		Expect(err).NotTo(HaveOccurred())
	})

	It("Should not fail during gendoc", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl gendoc")
		_, err := framework.ExecSimple(localExecutor, "_output/virtletctl", "gendoc", "/tmp")
		Expect(err).NotTo(HaveOccurred())
	})

	Context("Install subcommand", func() {
		It("Should not fail during install", func(done Done) {
			defer close(done)

			ctx, closeFunc := context.WithCancel(context.Background())
			defer closeFunc()
			localExecutor := framework.LocalExecutor(ctx)

			By("Calling virtletctl install")
			_, err := framework.ExecSimple(localExecutor, "_output/virtletctl", "install")
			Expect(err).NotTo(HaveOccurred())

			By("Calling kubectl plugin virt help")
			_, err = framework.ExecSimple(localExecutor, "kubectl", "plugin", "virt", "help")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("SSH subcommand", func() {
		var (
			vm           *framework.VMInterface
			tempfileName string
		)

		BeforeAll(func() {
			cm := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "sshkey",
				},
				Data: map[string]string{
					"authorized_keys": sshPublicKey,
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("cirros-vm")
			vm.Create(VMOptions{
				SSHKeySource: "configmap/sshkey",
			}.applyDefaults(), time.Minute*5, nil)

			waitSSH(vm)

			tempfile, err := ioutil.TempFile("", "")
			Expect(err).NotTo(HaveOccurred())
			defer tempfile.Close()
			tempfileName = tempfile.Name()

			strippedKey := strings.Replace(sshPrivateKey, "\t", "", -1)
			_, err = tempfile.Write([]byte(strippedKey))
			Expect(err).NotTo(HaveOccurred())
			Expect(os.Chmod(tempfileName, 0600)).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("sshkey", nil)
			os.Remove(tempfileName)
		})

		It("Should can call remote command with virtletctl ssh", func(done Done) {
			By("Calling virtletctl ssh cirros@cirros-vm hostname")
			defer close(done)

			ctx, closeFunc := context.WithCancel(context.Background())
			defer closeFunc()
			localExecutor := framework.LocalExecutor(ctx)

			output, err := framework.ExecSimple(localExecutor, "_output/virtletctl", "ssh", "cirros@cirros-vm", "--", "-i", tempfileName, "hostname")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("cirros-vm"))
		})
	})

	It("Should not fail during virsh list", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl virsh list")
		_, err := framework.ExecSimple(localExecutor, "_output/virtletctl", "virsh", "list")
		Expect(err).NotTo(HaveOccurred())
	})
})
