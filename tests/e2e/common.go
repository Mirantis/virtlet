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
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
)

// scheduleWaitSSH schedules SSH interface initialization before the test context starts
func scheduleWaitSSH(vm **framework.VMInterface, ssh *framework.Executor) {
	BeforeAll(func() {
		Eventually(
			func() error {
				var err error
				*ssh, err = (*vm).SSH("cirros", sshPrivateKey)
				if err != nil {
					return err
				}
				_, err = framework.ExecSimple(*ssh)
				return err
			}, 60*5, 3).Should(Succeed())
	})

	AfterAll(func() {
		(*ssh).Close()
	})
}

func checkCPUCount(vm *framework.VMInterface, ssh framework.Executor, cpus int) {
	proc := do(framework.ExecSimple(ssh, "cat", "/proc/cpuinfo")).(string)
	Expect(regexp.MustCompile(`(?m)^processor`).FindAllString(proc, -1)).To(HaveLen(cpus))
	cpuStats := do(vm.VirshCommand("domstats", "<domain>", "--vcpu")).(string)
	match := regexp.MustCompile(`vcpu\.maximum=(\d+)`).FindStringSubmatch(cpuStats)
	Expect(match).To(HaveLen(2))
	Expect(strconv.Atoi(match[1])).To(Equal(cpus))
}

func deleteVM(vm *framework.VMInterface) {
	virtletPod, err := vm.VirtletPod()
	Expect(err).NotTo(HaveOccurred())

	domainName, err := vm.DomainName()
	Expect(err).NotTo(HaveOccurred())
	domainName = domainName[8:21] // extract 5d3f8619-fda4 from virtlet-5d3f8619-fda4-cirros-vm

	Expect(vm.Delete(time.Minute * 2)).To(Succeed())

	commands := map[string][]string{
		"domain": {"list", "--name"},
		"volume": {"vol-list", "--pool", "volumes"},
		"secret": {"secret-list"},
	}

	for key, cmd := range commands {
		Eventually(func() error {
			out, err := framework.RunVirsh(virtletPod, cmd...)
			if err != nil {
				return err
			}
			if strings.Contains(out, domainName) {
				return fmt.Errorf("%s ~%s~ was not deleted", key, domainName)
			}
			return nil
		}, "2m").Should(Succeed())
	}
}

func do(value interface{}, extra ...interface{}) interface{} {
	ExpectWithOffset(1, value, extra...).To(BeAnything())
	return value
}
