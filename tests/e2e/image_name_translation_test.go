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

	"github.com/Mirantis/virtlet/pkg/imagetranslation"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Image URL", func() {
	var vimName string

	BeforeAll(func() {
		vim, err := controller.CreateVirtletImageMapping(imagetranslation.VirtletImageMapping{
			ObjectMeta: meta_v1.ObjectMeta{
				GenerateName: "virtlet-e2e-",
			},
			Spec: imagetranslation.ImageTranslation{
				Rules: []imagetranslation.TranslationRule{
					{
						Name: "test-image",
						Endpoint: utils.Endpoint{
							Url: *cirrosLocation,
						},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		vimName = vim.Name()
	})

	AfterAll(func() {
		Expect(controller.DeleteVirtletImageMapping(vimName)).NotTo(HaveOccurred())
	})

	It("Can be specified in CRD", func() {
		vm := controller.VM("cirros-vm-with-remapped-image")
		vm.Create(framework.VMOptions{
			Image:      "test-image",
			VCPUCount:  1,
			DiskDriver: "virtio",
		}, time.Minute*5, nil)
		_, err := vm.Pod()
		Expect(err).NotTo(HaveOccurred())
	})
})
