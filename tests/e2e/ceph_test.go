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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

const cephContainerName = "ceph_cluster"

var _ = Describe("Ceph volumes tests", func() {
	var (
		monitorIP string
		secret    string
	)

	BeforeAll(func() {
		monitorIP, secret = setupCeph()
	})

	AfterAll(func() {
		container, err := controller.DockerContainer(cephContainerName)
		Expect(err).NotTo(HaveOccurred())
		container.Delete()
	})

	Context("RBD volumes", func() {
		var (
			vm *framework.VMInterface
		)

		BeforeAll(func() {
			vm = controller.VM("cirros-vm-rbd")
			podCustomization := func(pod *framework.PodInterface) {
				pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
					Name:         "test1",
					VolumeSource: v1.VolumeSource{FlexVolume: cephVolume("rbd-test-image1", monitorIP, secret)},
				})
				pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
					Name:         "test2",
					VolumeSource: v1.VolumeSource{FlexVolume: cephVolume("rbd-test-image2", monitorIP, secret)},
				})
			}

			vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, podCustomization)
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
				checkFilesystemAccess(ssh)
			})
		})
	})

	Context("RBD volumes defined with PV/PVC", func() {
		var (
			vm *framework.VMInterface
		)

		BeforeAll(func() {
			pv := &v1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rbd-pv-virtlet",
				},
				Spec: v1.PersistentVolumeSpec{
					Capacity: v1.ResourceList{
						v1.ResourceStorage: resource.MustParse("10M"),
					},
					AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
					PersistentVolumeSource: v1.PersistentVolumeSource{
						FlexVolume: cephVolume("rbd-test-image-pv", monitorIP, secret),
					},
				},
			}
			do(controller.PersistentVolumesClient().Create(pv))

			pvc := &v1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rbd-claim",
				},
				Spec: v1.PersistentVolumeClaimSpec{
					AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceStorage: resource.MustParse("10M"),
						},
					},
				},
			}
			do(controller.PersistentVolumeClaimsClient().Create(pvc))

			vm = controller.VM("cirros-vm-rbd-pv")
			podCustomization := func(pod *framework.PodInterface) {
				pod.Pod.Spec.Volumes = append(pod.Pod.Spec.Volumes, v1.Volume{
					Name: "test",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: "rbd-claim",
						},
					},
				})
			}

			vm.Create(VMOptions{}.applyDefaults(), time.Minute*5, podCustomization)
			do(vm.Pod()).(*framework.PodInterface)
		})

		AfterAll(func() {
			controller.PersistentVolumeClaimsClient().Delete("rbd-claim", nil)
			controller.PersistentVolumesClient().Delete("rbd-pv-virtlet", nil)
			deleteVM(vm)
		})

		It("Must be attached to libvirt domain", func() {
			out := do(vm.VirshCommand("domblklist", "<domain>")).(string)
			Expect(regexp.MustCompile("(?m:rbd-test-image-pv$)").MatchString(out)).To(BeTrue())
		})

		Context("Mounted volumes", func() {
			var ssh framework.Executor
			scheduleWaitSSH(&vm, &ssh)

			It("Must be accessible from within OS", func() {
				checkFilesystemAccess(ssh)
			})
		})
	})
})

func checkFilesystemAccess(ssh framework.Executor) {
	do(framework.ExecSimple(ssh, "sudo /usr/sbin/mkfs.ext2 /dev/vdb"))
	do(framework.ExecSimple(ssh, "sudo mount /dev/vdb /mnt"))
	out := do(framework.ExecSimple(ssh, "ls -l /mnt")).(string)
	Expect(out).To(ContainSubstring("lost+found"))
}

func setupCeph() (string, string) {
	nodeExecutor, err := controller.DinDNodeExecutor("kube-master")
	Expect(err).NotTo(HaveOccurred())

	route, err := framework.ExecSimple(nodeExecutor, "route")
	Expect(err).NotTo(HaveOccurred())

	match := regexp.MustCompile(`default\s+([\d.]+)`).FindStringSubmatch(route)
	Expect(match).To(HaveLen(2))

	monIP := match[1]
	cephPublicNetwork := monIP + "/16"

	container, err := controller.DockerContainer(cephContainerName)
	Expect(err).NotTo(HaveOccurred())

	container.Delete()
	Expect(container.PullImage("ceph/demo:tag-stable-3.0-jewel-ubuntu-16.04")).To(Succeed())
	Expect(container.Run("ceph/demo:tag-stable-3.0-jewel-ubuntu-16.04",
		map[string]string{"MON_IP": monIP, "CEPH_PUBLIC_NETWORK": cephPublicNetwork},
		"host", nil, false)).To(Succeed())

	cephContainerExecutor := container.Executor(false, "")
	By("Waiting for ceph cluster")
	Eventually(func() error {
		_, err := framework.ExecSimple(cephContainerExecutor, "ceph", "-s")
		return err
	}).Should(Succeed())
	By("Ceph cluster started")

	var out string
	commands := []string{
		// Adjust ceph configs
		`echo -e "rbd default features = 1\nrbd default format = 2" >> /etc/ceph/ceph.conf`,

		// Add rbd pool and volume
		`ceph osd pool create libvirt-pool 8 8`,
		`apt-get update && apt-get install -y qemu-utils 2> /dev/null`,
		`qemu-img create -f rbd rbd:libvirt-pool/rbd-test-image1 10M`,
		`qemu-img create -f rbd rbd:libvirt-pool/rbd-test-image2 10M`,
		`qemu-img create -f rbd rbd:libvirt-pool/rbd-test-image-pv 10M`,

		// Add user for virtlet
		`ceph auth get-or-create client.libvirt`,
		`ceph auth caps client.libvirt mon "allow *" osd "allow *" msd "allow *"`,
		`ceph auth get-key client.libvirt`,
	}
	for _, cmd := range commands {
		out = do(framework.ExecSimple(cephContainerExecutor, "/bin/bash", "-c", cmd)).(string)
	}
	return monIP, out
}

func cephVolume(volume, monitorIP, secret string) *v1.FlexVolumeSource {
	return &v1.FlexVolumeSource{
		Driver: "virtlet/flexvolume_driver",
		Options: map[string]string{
			"type":    "ceph",
			"monitor": monitorIP + ":6789",
			"user":    "libvirt",
			"secret":  secret,
			"volume":  volume,
			"pool":    "libvirt-pool",
		},
	}
}
