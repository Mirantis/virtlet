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
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	virtlet_v1 "github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Image URL", func() {
	var vimName string

	BeforeAll(func() {
		vim, err := controller.CreateVirtletImageMapping(virtlet_v1.VirtletImageMapping{
			ObjectMeta: meta_v1.ObjectMeta{
				GenerateName: "virtlet-e2e-",
			},
			Spec: virtlet_v1.ImageTranslation{
				Rules: []virtlet_v1.TranslationRule{
					{
						Name: "test-image",
						URL:  *vmImageLocation,
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		vimName = vim.Name
	})

	AfterAll(func() {
		Expect(controller.DeleteVirtletImageMapping(vimName)).To(Succeed())
	})

	It("Can be specified in CRD [Conformance]", func() {
		vm := controller.VM("cirros-vm-with-remapped-image")
		Expect(vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, nil)).To(Succeed())
		_, err := vm.Pod()
		Expect(err).NotTo(HaveOccurred())
		deleteVM(vm)
	})
})
