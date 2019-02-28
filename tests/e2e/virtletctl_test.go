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
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
		callVirtletctl(localExecutor, "help")
	}, 10)

	Context("Tests depending on spawning VM", func() {
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
					"authorized_keys": SshPublicKey,
				},
			}
			_, err := controller.ConfigMaps().Create(cm)
			Expect(err).NotTo(HaveOccurred())

			vm = controller.VM("virtletctl-test-vm")
			Expect(vm.CreateAndWait(VMOptions{
				SSHKeySource: "configmap/sshkey",
			}.ApplyDefaults(), time.Minute*5, nil)).To(Succeed())

			waitSSH(vm)

			tempfile, err := ioutil.TempFile("", "")
			Expect(err).NotTo(HaveOccurred())
			defer tempfile.Close()
			tempfileName = tempfile.Name()

			strippedKey := strings.Replace(SshPrivateKey, "\t", "", -1)
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

			output := callVirtletctl(localExecutor, "ssh", "--namespace", controller.Namespace(), *sshUser+"@virtletctl-test-vm", "--", "-i", tempfileName, "hostname")
			Expect(output).To(Equal("virtletctl-test-vm"))
		}, 60)

		It("Should dump Virtlet diagnostics on diag dump subcommand", func(done Done) {
			defer close(done)

			ctx, closeFunc := context.WithCancel(context.Background())
			defer closeFunc()
			localExecutor := framework.LocalExecutor(ctx)

			By("Calling virtletctl diag dump")
			tempDir, err := ioutil.TempDir("", "diag-out")
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				os.RemoveAll(tempDir)
			}()
			callVirtletctl(localExecutor, "diag", "dump", tempDir)
			metadataFiles, err := filepath.Glob(filepath.Join(tempDir, "nodes/*/metadata.txt"))
			Expect(err).NotTo(HaveOccurred())
			Expect(metadataFiles).NotTo(BeEmpty())
			content, err := ioutil.ReadFile(metadataFiles[0])
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("Sandboxes:"))

			By("Calling virtletctl diag dump --json")
			output := callVirtletctl(localExecutor, "diag", "dump", "--json")
			var decoded map[string]interface{}
			Expect(json.Unmarshal([]byte(output), &decoded)).To(Succeed())
			Expect(decoded["name"]).To(Equal("nodes"))
			Expect(decoded["children"]).To(BeAssignableToTypeOf(map[string]interface{}{}))
			m := decoded["children"].(map[string]interface{})
			nodeName := ""
			for name := range m {
				nodeName = name
				break
			}
			Expect(nodeName).NotTo(BeEmpty())
			Expect(m[nodeName]).To(BeAssignableToTypeOf(map[string]interface{}{}))
			m = m[nodeName].(map[string]interface{})
			Expect(m["children"]).To(BeAssignableToTypeOf(map[string]interface{}{}))
			m = m["children"].(map[string]interface{})
			Expect(m["metadata"]).To(BeAssignableToTypeOf(map[string]interface{}{}))
			m = m["metadata"].(map[string]interface{})
			Expect(m["data"]).To(ContainSubstring("Sandboxes:"))
		}, 60)
	})

	It("Should return libvirt version on virsh subcommand", func(done Done) {
		defer close(done)

		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl virsh version")
		output := callVirtletctl(localExecutor, "virsh", "version")
		Expect(output).To(ContainSubstring("Compiled against library:"))
		Expect(output).To(ContainSubstring("Using library:"))
	}, 60)

	It("Should install itself as a kubectl plugin on install subcommand [unsafe]", func() {
		includeUnsafe()
		ctx, closeFunc := context.WithCancel(context.Background())
		defer closeFunc()
		localExecutor := framework.LocalExecutor(ctx)

		By("Calling virtletctl install")
		callVirtletctl(localExecutor, "install")

		By("Calling kubectl plugin virt help")
		_, err := framework.RunSimple(localExecutor, "kubectl", "plugin", "virt", "help")
		Expect(err).NotTo(HaveOccurred())
	}, 60)
})

func callVirtletctl(executor framework.Executor, args ...string) string {
	args = append([]string{"_output/virtletctl", "-s", *framework.ClusterURL}, args...)
	output, err := framework.RunSimple(executor, args...)
	Expect(err).NotTo(HaveOccurred())
	return output
}
