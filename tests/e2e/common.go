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
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/Mirantis/virtlet/tests/e2e/framework"
	. "github.com/Mirantis/virtlet/tests/e2e/ginkgo-ext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	cephContainerName = "ceph_cluster"
	// avoid having the loop device on top of overlay2/aufs when using k-d-c
	loopDeviceTestDir = "/dind/virtlet-e2e-tests"
)

var (
	vmImageLocation       = flag.String("image", defaultVMImageLocation, "VM image URL (*without http(s)://*")
	sshUser               = flag.String("sshuser", DefaultSSHUser, "default SSH user for VMs")
	includeCloudInitTests = flag.Bool("include-cloud-init-tests", false, "include Cloud-Init tests")
	includeUnsafeTests    = flag.Bool("include-unsafe-tests", false, "include tests that can be unsafe if they're run outside the build container")
	memoryLimit           = flag.Int("memoryLimit", 160, "default VM memory limit (in MiB)")
	junitOutput           = flag.String("junitOutput", "", "JUnit XML output file")
	controller            *framework.Controller
)

// UsingCirros() returns true if cirros image is being used for tests
// (which has some limitations)
func UsingCirros() bool {
	return strings.Contains(*vmImageLocation, "cirros")
}

// scheduleWaitSSH schedules SSH interface initialization before the test context starts
func scheduleWaitSSH(vm **framework.VMInterface, ssh *framework.Executor) {
	BeforeAll(func() {
		*ssh = waitSSH(*vm)
	})

	AfterAll(func() {
		(*ssh).Close()
	})
}

func waitSSH(vm *framework.VMInterface) framework.Executor {
	var ssh framework.Executor
	Eventually(
		func() error {
			var err error
			ssh, err = vm.SSH(*sshUser, SshPrivateKey)
			if err != nil {
				return err
			}
			_, err = framework.RunSimple(ssh)
			return err
		}, 60*5, 3).Should(Succeed())
	return ssh
}

func waitVirtletPod(vm *framework.VMInterface) *framework.PodInterface {
	var virtletPod *framework.PodInterface
	Eventually(
		func() error {
			var err error
			virtletPod, err = vm.VirtletPod()
			if err != nil {
				return err
			}
			for _, c := range virtletPod.Pod.Status.Conditions {
				if c.Type == v1.PodReady && c.Status == v1.ConditionTrue {
					return nil
				}
			}
			return fmt.Errorf("Pod not ready yet: %+v", virtletPod.Pod.Status)
		}, 60*5, 3).Should(Succeed())
	return virtletPod
}

func checkCPUCount(vm *framework.VMInterface, ssh framework.Executor, cpus int) {
	proc := do(framework.RunSimple(ssh, "cat", "/proc/cpuinfo")).(string)
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
		}, "3m").Should(Succeed())
	}
}

// do asserts that function with multiple return values doesn't fail
// Considering we have func `foo(something) (something, error)`
//
// `x := do(foo(something))` is equivalent to
// val, err := fn(something)
// Expect(err).To(Succeed())
// x = val
//
// The rule is that the function must return at least 2 values,
// and the last value is interpreted as error.
func do(value interface{}, extra ...interface{}) interface{} {
	if len(extra) == 0 {
		panic("bad usage of do() -- no extra values")
	}
	lastValue := extra[len(extra)-1]
	if lastValue != nil {
		err := lastValue.(error)
		Expect(err).NotTo(HaveOccurred())
	}
	return value
}

type VMOptions framework.VMOptions

func (o VMOptions) ApplyDefaults() framework.VMOptions {
	res := framework.VMOptions(o)
	if res.Image == "" {
		res.Image = *vmImageLocation
	}
	if res.SSHKey == "" && res.SSHKeySource == "" {
		res.SSHKey = SshPublicKey
	}
	if res.VCPUCount == 0 {
		res.VCPUCount = 1
	}
	if res.DiskDriver == "" {
		res.DiskDriver = "virtio"
	}
	if res.Limits == nil {
		res.Limits = map[string]string{}
	}
	if res.Limits["memory"] == "" {
		res.Limits["memory"] = fmt.Sprintf("%dMi", *memoryLimit)
	}

	return res
}

func requireCloudInit() {
	if !*includeCloudInitTests {
		Skip("Cloud-Init tests are not enabled")
	}
}

func includeUnsafe() {
	if !*includeUnsafeTests {
		Skip("Tests that are unsafe outside the build container are disabled")
	}
}

func withLoopbackBlockDevice(virtletNodeName, devPath *string, mkfs bool) {
	var nodeExecutor framework.Executor
	var filename string
	BeforeEach(func() {
		var err error
		*virtletNodeName, err = controller.VirtletNodeName()
		Expect(err).NotTo(HaveOccurred())
		nodeExecutor, err = controller.DinDNodeExecutor(*virtletNodeName)
		Expect(err).NotTo(HaveOccurred())

		_, err = framework.RunSimple(nodeExecutor, "mkdir", "-p", loopDeviceTestDir)
		Expect(err).NotTo(HaveOccurred())

		filename, err = framework.RunSimple(nodeExecutor, "tempfile", "-d", loopDeviceTestDir, "--prefix", "ve2e-")
		Expect(err).NotTo(HaveOccurred())

		_, err = framework.RunSimple(nodeExecutor, "dd", "if=/dev/zero", "of="+filename, "bs=1M", "count=1000")
		Expect(err).NotTo(HaveOccurred())
		if mkfs {
			// We use mkfs.ext3 here because mkfs.ext4 on
			// the node may be too new for CirrOS, causing
			// errors like this in VM's dmesg:
			// [    1.316395] EXT3-fs (vdb): error: couldn't mount because of unsupported optional features (2c0)
			// [    1.320222] EXT4-fs (vdb): couldn't mount RDWR because of unsupported optional features (400)
			// [    1.339594] EXT3-fs (vdc1): error: couldn't mount because of unsupported optional features (240)
			// [    1.342850] EXT4-fs (vdc1): mounted filesystem with ordered data mode. Opts: (null)
			_, err = framework.RunSimple(nodeExecutor, "mkfs.ext3", filename)
			Expect(err).NotTo(HaveOccurred())
		}
		_, err = framework.RunSimple(nodeExecutor, "sync")
		Expect(err).NotTo(HaveOccurred())
		*devPath, err = framework.RunSimple(nodeExecutor, "losetup", "-f", filename, "--show")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// The loopback device is detached by itself upon
		// success (TODO: check why it happens), so we
		// ignore errors here
		framework.RunSimple(nodeExecutor, "losetup", "-d", *devPath)
		Expect(os.RemoveAll(loopDeviceTestDir)).To(Succeed())
	})
}

func withCeph(monitorIP, secret *string, kubeSecret string) {
	BeforeAll(func() {
		nodeExecutor, err := (*controller).DinDNodeExecutor("kube-master")
		Expect(err).NotTo(HaveOccurred())

		route, err := framework.RunSimple(nodeExecutor, "route", "-n")
		Expect(err).NotTo(HaveOccurred())

		match := regexp.MustCompile(`(?:default|0\.0\.0\.0)\s+([\d.]+)`).FindStringSubmatch(route)
		Expect(match).To(HaveLen(2))

		*monitorIP = match[1]
		cephPublicNetwork := *monitorIP + "/16"

		container, err := controller.DockerContainer(cephContainerName)
		Expect(err).NotTo(HaveOccurred())

		container.Delete()
		Expect(container.PullImage("docker.io/ceph/daemon:v3.1.0-stable-3.1-mimic-centos-7")).To(Succeed())
		Expect(container.Run("docker.io/ceph/daemon:v3.1.0-stable-3.1-mimic-centos-7",
			map[string]string{
				"MON_IP":               *monitorIP,
				"CEPH_PUBLIC_NETWORK":  cephPublicNetwork,
				"CEPH_DEMO_UID":        "foo",
				"CEPH_DEMO_ACCESS_KEY": "foo",
				"CEPH_DEMO_SECRET_KEY": "foo",
				"CEPH_DEMO_BUCKET":     "foo",
				"DEMO_DAEMONS":         "osd mds",
			},
			"host", nil, false, "demo")).To(Succeed())

		cephContainerExecutor := container.Executor(false, "")
		By("Waiting for ceph cluster")
		Eventually(func() error {
			_, err := framework.RunSimple(cephContainerExecutor, "ceph", "-s")
			return err
		}).Should(Succeed())
		By("Ceph cluster started")

		commands := []string{
			// Add rbd pool and volume
			`ceph osd pool create libvirt-pool 8 8`,
			`rbd create rbd-test-image1 --size 1G --pool libvirt-pool --image-feature layering`,
			`rbd create rbd-test-image2 --size 1G --pool libvirt-pool --image-feature layering`,
			`rbd create rbd-test-image-pv --size 1G --pool libvirt-pool --image-feature layering`,

			// Add user for virtlet
			`ceph auth get-key client.admin`,
		}
		var out string
		for _, cmd := range commands {
			out = do(framework.RunSimple(cephContainerExecutor, "/bin/bash", "-c", cmd)).(string)
		}
		if secret != nil {
			*secret = out
		}
		if kubeSecret != "" {
			// buf := bytes.NewBufferString(out)
			// decoder := base64.NewDecoder(base64.StdEncoding, buf)
			// decoded, err := ioutil.ReadAll(decoder)
			// Expect(err).NotTo(HaveOccurred())
			_, err = controller.Secrets().Create(&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: kubeSecret,
				},
				Type: "kubernetes.io/rbd",
				Data: map[string][]byte{
					"key": []byte(out),
				},
			})
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterAll(func() {
		container, err := controller.DockerContainer(cephContainerName)
		Expect(err).NotTo(HaveOccurred())
		container.Delete()
		if kubeSecret != "" {
			Expect(controller.Secrets().Delete(kubeSecret, nil)).To(Succeed())
			Eventually(func() error {
				if _, err := controller.Secrets().Get(kubeSecret, metav1.GetOptions{}); err != nil {
					if k8serrors.IsNotFound(err) {
						return nil
					}
					return err
				}
				return fmt.Errorf("secret %s was not deleted", kubeSecret)
			}).Should(Succeed())
		}
	})
}

func makeVMWithMountAndSymlinkScript(nodeName string, PVCs []framework.PVCSpec, podCustomization func(*framework.PodInterface)) *framework.VMInterface {
	vm := controller.VM("mount-vm")
	Expect(vm.CreateAndWait(VMOptions{
		NodeName: nodeName,
		// TODO: should also have an option to test using
		// ubuntu image with volumes mounted using cloud-init
		// userdata 'mounts' section
		UserDataScript: "@virtlet-mount-script@",
		PVCs:           PVCs,
	}.ApplyDefaults(), time.Minute*5, podCustomization)).To(Succeed())
	_, err := vm.Pod()
	Expect(err).NotTo(HaveOccurred())
	return vm
}

func expectToBeUsableForFilesystem(ssh framework.Executor, devPath string) {
	Eventually(func() error {
		_, err := framework.RunSimple(ssh, fmt.Sprintf("sudo /usr/sbin/mkfs.ext2 %s", devPath))
		return err
	}, 60*5, 3).Should(Succeed())
	do(framework.RunSimple(ssh, fmt.Sprintf("sudo mount %s /mnt", devPath)))
	out := do(framework.RunSimple(ssh, "ls -l /mnt")).(string)
	Expect(out).To(ContainSubstring("lost+found"))
}
