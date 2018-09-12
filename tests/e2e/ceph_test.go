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
	"regexp"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/api/core/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Ceph volumes tests", func() {
	var (
		monitorIP string
		secret    string
	)

	withCeph(&monitorIP, &secret, "")

	Context("RBD volumes", func() {
		var (
			vm *framework.VMInterface
		)

		BeforeAll(func() {
			vm = controller.VM("cirros-vm-rbd")
			podCustomization := func(pod *framework.PodInterface) {
				pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
					Name:         "test1",
					VolumeSource: v1.VolumeSource{FlexVolume: cephVolumeSource("rbd-test-image1", monitorIP, secret)},
				})
				pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
					Name:         "test2",
					VolumeSource: v1.VolumeSource{FlexVolume: cephVolumeSource("rbd-test-image2", monitorIP, secret)},
				})
			}

			Expect(vm.CreateAndWait(VMOptions{}.ApplyDefaults(), time.Minute*5, podCustomization)).To(Succeed())
			var err error
			_, err = vm.Pod()
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			deleteVM(vm)
		})

		It("Must be attached to libvirt domain", func() {
			out, err := vm.VirshCommand("domblklist", "<domain>")
			Expect(err).NotTo(HaveOccurred())
			match := regexp.MustCompile("(?m:rbd-test-image[12]$)").FindAllString(out, -1)
			Expect(match).To(HaveLen(2))
		})

		Context("Mounted volumes", func() {
			var ssh framework.Executor
			scheduleWaitSSH(&vm, &ssh)

			It("Must be accessible from within OS", func() {
				expectToBeUsableForFilesystem(ssh, "/dev/vdb")
			})
		})
	})

	Context("RBD volumes defined with PV/PVC", func() {
		var (
			vm *framework.VMInterface
		)

		BeforeAll(func() {
			vm = controller.VM("cirros-vm-rbd-pv")
			opts := VMOptions{
				PVCs: []framework.PVCSpec{
					{
						Name:              "rbd-pv-virtlet",
						Size:              "10M",
						FlexVolumeOptions: cephOptions("rbd-test-image-pv", monitorIP, secret),
					},
				},
			}.ApplyDefaults()
			Expect(vm.CreateAndWait(opts, time.Minute*5, nil)).To(Succeed())
			_ = do(vm.Pod()).(*framework.PodInterface)
		})

		AfterAll(func() {
			deleteVM(vm)
		})

		It("Must be attached to libvirt domain", func() {
			out := do(vm.VirshCommand("domblklist", "<domain>")).(string)
			Expect(regexp.MustCompile("(?m:rbd-test-image-pv$)").MatchString(out)).To(BeTrue())
		})

		It("Must be accessible from within the VM", func() {
			ssh := waitSSH(vm)
			expectToBeUsableForFilesystem(ssh, "/dev/vdb")
		})
	})
})

func cephOptions(volume, monitorIP, secret string) map[string]string {
	return map[string]string{
		"type":    "ceph",
		"monitor": monitorIP + ":6789",
		"user":    "admin",
		"secret":  secret,
		"volume":  volume,
		"pool":    "libvirt-pool",
	}
}

func cephVolumeSource(volume, monitorIP, secret string) *v1.FlexVolumeSource {
	return &v1.FlexVolumeSource{
		Driver:  "virtlet/flexvolume_driver",
		Options: cephOptions(volume, monitorIP, secret),
	}
}

// TODO: use client.admin instead of client.libvirt
