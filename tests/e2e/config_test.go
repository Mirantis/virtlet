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
	. "github.com/onsi/gomega"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	virtlet_v1 "github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Per-node configuration", func() {
	var extraVirtletNode string
	var virtletPod *framework.PodInterface
	var configMappingNames []string

	installVirtletOnExtraNode := func() {
		var err error
		extraVirtletNode, err = controller.AvailableNodeName()
		Expect(err).NotTo(HaveOccurred())
		Expect(controller.AddLabelsToNode(extraVirtletNode, map[string]string{
			"extraRuntime": "virtlet",
			"foobarConfig": "baz",
		})).To(Succeed())
		virtletPod, err = controller.WaitForVirtletPodOnTheNode(extraVirtletNode)
		Expect(err).NotTo(HaveOccurred())
	}

	createConfigs := func() {
		logLevel := 5
		rawDevs := "foobar*"
		cms := []virtlet_v1.VirtletConfigMapping{
			{
				ObjectMeta: meta_v1.ObjectMeta{
					GenerateName: "virtlet-e2e-",
				},
				Spec: virtlet_v1.VirtletConfigMappingSpec{
					NodeSelector: map[string]string{
						"extraRuntime": "virtlet",
					},
					Config: &virtlet_v1.VirtletConfig{
						LogLevel: &logLevel,
					},
				},
			},
			{
				ObjectMeta: meta_v1.ObjectMeta{
					GenerateName: "virtlet-e2e-",
				},
				Spec: virtlet_v1.VirtletConfigMappingSpec{
					NodeSelector: map[string]string{
						"foobarConfig": "baz",
					},
					Config: &virtlet_v1.VirtletConfig{
						RawDevices: &rawDevs,
					},
				},
			},
		}
		for _, cm := range cms {
			cm, err := controller.CreateVirtletConfigMapping(cm)
			Expect(err).NotTo(HaveOccurred())
			if cm != nil {
				configMappingNames = append(configMappingNames, cm.Name)
			}
		}
	}

	AfterAll(func() {
		if extraVirtletNode != "" {
			Expect(controller.RemoveLabelOffNode(extraVirtletNode, []string{
				"extraRuntime",
				"foobarConfig",
			})).To(Succeed())
			Expect(controller.WaitForVirtletPodToDisappearFromTheNode(extraVirtletNode)).To(Succeed())
		}
		for _, cmName := range configMappingNames {
			Expect(controller.DeleteVirtletConfigMapping(cmName)).To(Succeed())
		}
	})

	It("should be obtained by combining the Virtlet config mappings that match the node", func() {
		createConfigs()
		installVirtletOnExtraNode()
		virtletContainer, err := virtletPod.Container("virtlet")
		Expect(err).NotTo(HaveOccurred())
		out, err := framework.RunSimple(virtletContainer, "cat", "/var/lib/virtlet/config.sh")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("export VIRTLET_RAW_DEVICES=foobar\\*\n"))
		Expect(out).To(ContainSubstring("export VIRTLET_LOGLEVEL=5"))
	})
})
