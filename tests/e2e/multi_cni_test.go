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
	"fmt"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

const (
	ensureEth1UpCmd = "export PATH='/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin';" +
		"(ip a show dev eth1 | grep -qw inet) || " +
		"( (echo -e 'iface eth1 inet dhcp' | " +
		"sudo tee -a /etc/network/interfaces) && sudo /sbin/ifup eth1)"
	getLinkIpCmd = "export PATH='/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin';" +
		"ip a show dev eth%d | grep -w inet | " +
		"sed 's@/.*@@' | awk '{ print $2; }'"
	netcatListenCmd = "echo iwaslistening | nc -l -p 12345 | grep isentthis"
	netcatSendCmd   = "echo isentthis | nc -w5 %s 12345 | grep iwaslistening"
)

var _ = Describe("VMs with multiple CNIs using CNI Genie [MultiCNI]", func() {
	describeMultiCNI("With 'cni' annotation", true)
	describeMultiCNI("Without 'cni' annotation", false)
})

func describeMultiCNI(what string, addCNIAnnotation bool) {
	Context(what, func() {
		var (
			vms [2]*multiCNIVM
		)
		BeforeAll(func() {
			for i := 0; i < 2; i++ {
				vms[i] = makeMultiCNIVM(fmt.Sprintf("vm%d", i), addCNIAnnotation)
				vms[i].ensureIPOnSecondEth()
				vms[i].retrieveIPs()
			}
		})

		AfterAll(func() {
			for _, vm := range vms {
				vm.teardown()
			}
		})

		It("Should have connectivity between them on all the CNI-provided interfaces inside VMs", func() {
			vms[0].ping(vms[1].ips[0])
			vms[0].ping(vms[1].ips[1])
			vms[1].ping(vms[0].ips[0])
			vms[1].ping(vms[0].ips[1])
			errCh := make(chan error)
			go func() {
				errCh <- vms[0].netcatListen(vms[0].ips[0])
			}()
			vms[1].netcatConnect(vms[0].ips[0])
			Expect(<-errCh).To(Succeed())
			go func() {
				errCh <- vms[0].netcatListen(vms[0].ips[1])
			}()
			vms[1].netcatConnect(vms[0].ips[1])
			Expect(<-errCh).To(Succeed())
		})

		itShouldHaveNetworkConnectivity(
			func() *framework.PodInterface { return vms[0].vmPod },
			func() framework.Executor { return vms[0].ssh })
	})
}

type multiCNIVM struct {
	vm    *framework.VMInterface
	vmPod *framework.PodInterface
	ssh   framework.Executor
	ips   [2]string
}

func makeMultiCNIVM(name string, addCNIAnnotation bool) *multiCNIVM {
	vm := controller.VM(name)
	opts := VMOptions{}
	if addCNIAnnotation {
		opts.MultiCNI = "calico,flannel"
	}
	Expect(vm.Create(opts.applyDefaults(), time.Minute*5, nil)).To(Succeed())
	vmPod, err := vm.Pod()
	Expect(err).NotTo(HaveOccurred())
	return &multiCNIVM{
		vm:    vm,
		vmPod: vmPod,
		ssh:   waitSSH(vm),
	}
}

func (mcv *multiCNIVM) ensureIPOnSecondEth() {
	_, err := framework.RunSimple(mcv.ssh, ensureEth1UpCmd)
	Expect(err).NotTo(HaveOccurred())
}

func (mcv *multiCNIVM) retrieveIPs() {
	for i := 0; i < 2; i++ {
		ip, err := framework.RunSimple(mcv.ssh, fmt.Sprintf(getLinkIpCmd, i))
		Expect(err).NotTo(HaveOccurred())
		mcv.ips[i] = ip
	}
}

func (mcv *multiCNIVM) teardown() {
	if mcv.ssh != nil {
		mcv.ssh.Close()
	}
	if mcv.vm != nil {
		deleteVM(mcv.vm)
	}
}

func (mcv *multiCNIVM) ping(ip string) {
	Expect(framework.RunSimple(mcv.ssh, fmt.Sprintf("ping -c1 %s", ip))).
		To(MatchRegexp("1 .*transmitted, 1 .*received, 0% .*loss"))
}

func (mcv *multiCNIVM) netcatListen(listenIp string) error {
	_, err := framework.RunSimple(mcv.ssh, netcatListenCmd)
	return err
}

func (mcv *multiCNIVM) netcatConnect(targetIp string) {
	Eventually(func() error {
		_, err := framework.RunSimple(mcv.ssh, fmt.Sprintf(netcatSendCmd, targetIp))
		return err
	}, 60).Should(Succeed())
}
