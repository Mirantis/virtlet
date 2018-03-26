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
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

const (
	gendocTmpDir = "/tmp"
)

var _ = Describe("virtletctl", func() {
	It("Should display usage info on help subcommand", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl help")
		_, err := framework.RunSimple(localExecutor, "_output/virtletctl", "help")
		Expect(err).NotTo(HaveOccurred())
	}, 10)

	It("Should dump Virtlet metadata on dump-metadata subcommand", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl dump-metadata")
		output, err := framework.RunSimple(localExecutor, "_output/virtletctl", "dump-metadata")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("Virtlet pod name:"))
		Expect(output).To(ContainSubstring("Sandboxes:"))
		Expect(output).To(ContainSubstring("Images:"))
	}, 60)

	It("Should generate documentation on gendoc subcommand", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl gendoc")
		_, err := framework.RunSimple(localExecutor, "_output/virtletctl", "gendoc", gendocTmpDir)
		Expect(err).NotTo(HaveOccurred())
		content, err := ioutil.ReadFile(filepath.Join(gendocTmpDir, "virtletctl.md"))
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(ContainSubstring("Virtlet control tool"))
		Expect(content).To(ContainSubstring("Synopsis"))
		Expect(content).To(ContainSubstring("Options"))
	}, 10)

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
			Expect(os.Chmod(tempfileName, 0600)).To(Succeed())
		})

		AfterAll(func() {
			deleteVM(vm)
			controller.ConfigMaps().Delete("sshkey", nil)
			os.Remove(tempfileName)
		})

		It("Should be able to access the pod via ssh using ssh subcommand", func(done Done) {
			By("Calling virtletctl ssh cirros@cirros-vm hostname")
			defer close(done)

			ctx, closeFunc := context.WithCancel(context.Background())
			defer closeFunc()
			localExecutor := framework.LocalExecutor(ctx)

			output, err := framework.RunSimple(localExecutor, "_output/virtletctl", "ssh", "cirros@cirros-vm", "--", "-i", tempfileName, "hostname")
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("cirros-vm"))
		}, 60)
	})

	It("Should return libvirt version on virsh subcommand", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl virsh version")
		output, err := framework.RunSimple(localExecutor, "_output/virtletctl", "virsh", "version")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("Compiled against library:"))
		Expect(output).To(ContainSubstring("Using library:"))
	}, 60)
})

var _ = Describe("virtletctl unsafe", func() {
	BeforeAll(func() {
		includeUnsafe()
	})

	Context("Should install itself as a kubectl plugin on install subcommand", func() {
		It("Should not fail during install", func(done Done) {
			defer close(done)

			ctx, closeFunc := context.WithCancel(context.Background())
			defer closeFunc()
			localExecutor := framework.LocalExecutor(ctx)

			By("Calling virtletctl install")
			_, err := framework.RunSimple(localExecutor, "_output/virtletctl", "install")
			Expect(err).NotTo(HaveOccurred())

			By("Calling kubectl plugin virt help")
			_, err = framework.RunSimple(localExecutor, "kubectl", "plugin", "virt", "help")
			Expect(err).NotTo(HaveOccurred())
		}, 60)
	})
})
