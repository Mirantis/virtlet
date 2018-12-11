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

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

var _ = Describe("Block PVs", func() {
	var (
		vm  *framework.VMInterface
		ssh framework.Executor
	)

	describePersistentRootfs := func(makePersistentRootVM func()) {
		It("[Flaky] Should be usable as a persistent root filesystem", func() {
			makePersistentRootVM()
			ssh = waitSSH(vm)
			do(framework.RunSimple(ssh, "echo -n foo | sudo tee /bar.txt"))
			Expect(ssh.Close()).To(Succeed())
			deleteVM(vm)
			makePersistentRootVM()
			ssh = waitSSH(vm)
			out, err := framework.RunSimple(ssh, "sudo cat /bar.txt")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("foo"))
		})
	}

	Context("[Local]", func() {
		var (
			virtletNodeName string
			devPath         string
		)

		AfterEach(func() {
			if ssh != nil {
				ssh.Close()
			}
			if vm != nil {
				deleteVM(vm)
			}
		})

		Context("[Non-root]", func() {
			withLoopbackBlockDevice(&virtletNodeName, &devPath, true)
			It("Should be accessible from within the VM", func() {
				vm = makeVMWithMountAndSymlinkScript(virtletNodeName, []framework.PVCSpec{
					{
						Name:          "block-pv",
						Size:          "10M",
						NodeName:      virtletNodeName,
						Block:         true,
						LocalPath:     devPath,
						ContainerPath: "/dev/testpvc",
					},
				}, nil)
				ssh = waitSSH(vm)
				expectToBeUsableForFilesystem(ssh, "/dev/testpvc")
			})
		})

		Context("[Root]", func() {
			withLoopbackBlockDevice(&virtletNodeName, &devPath, false)
			describePersistentRootfs(func() {
				vm = makeVMWithMountAndSymlinkScript(virtletNodeName, []framework.PVCSpec{
					{
						Name:          "block-pv",
						Size:          "10M",
						NodeName:      virtletNodeName,
						Block:         true,
						LocalPath:     devPath,
						ContainerPath: "/",
					},
				}, nil)
			})
		})
	})

	Context("[Disruptive][Ceph RBD]", func() {
		var monitorIP string
		withCeph(&monitorIP, nil, "ceph-admin")

		AfterEach(func() {
			if ssh != nil {
				ssh.Close()
			}
			if vm != nil {
				deleteVM(vm)
			}
		})

		// FIXME: the test is marked Disruptive because rbd
		// hangs on CircleCI for some reason.
		It("Should be accessible from within the VM", func() {
			vm = makeVMWithMountAndSymlinkScript("", []framework.PVCSpec{
				{
					Name:           "block-pv",
					Size:           "10M",
					Block:          true,
					CephRBDImage:   "rbd-test-image1",
					CephMonitorIP:  monitorIP,
					CephRBDPool:    "libvirt-pool",
					CephSecretName: "ceph-admin",
					ContainerPath:  "/dev/testpvc",
				},
			}, nil)
			ssh = waitSSH(vm)
			expectToBeUsableForFilesystem(ssh, "/dev/testpvc")
		})

		describePersistentRootfs(func() {
			vm = makeVMWithMountAndSymlinkScript("", []framework.PVCSpec{
				{
					Name:           "block-pv",
					Size:           "10M",
					Block:          true,
					CephRBDImage:   "rbd-test-image1",
					CephMonitorIP:  monitorIP,
					CephRBDPool:    "libvirt-pool",
					CephSecretName: "ceph-admin",
					ContainerPath:  "/",
				},
			}, nil)
		})
	})
})
