/*
Copyright 2016-2017 Mirantis

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

package libvirttools

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libvirt/libvirt-go-xml"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/Mirantis/virtlet/pkg/flexvolume"
	"github.com/Mirantis/virtlet/pkg/imagetranslation"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/criapi"
	"github.com/Mirantis/virtlet/tests/gm"
)

const (
	fakeImageName        = "fake/image1"
	fakeCNIConfig        = `{"noCniForNow":true}`
	fakeUuid             = "abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"
	fakeContainerName    = "container1"
	fakeContainerAttempt = 42
	stopContainerTimeout = 30 * time.Second
)

type containerTester struct {
	t              *testing.T
	clock          clockwork.FakeClock
	tmpDir         string
	kubeletRootDir string
	virtTool       *VirtualizationTool
	rec            *fake.TopLevelRecorder
	domainConn     *fake.FakeDomainConnection
	storageConn    *fake.FakeStorageConnection
	metadataStore  metadata.MetadataStore
}

func newContainerTester(t *testing.T, rec *fake.TopLevelRecorder) *containerTester {
	ct := &containerTester{
		t:     t,
		clock: clockwork.NewFakeClockAt(time.Date(2017, 5, 30, 20, 19, 0, 0, time.UTC)),
	}

	var err error
	ct.tmpDir, err = ioutil.TempDir("", "virtualization-test-")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}

	downloader := utils.NewFakeDownloader(ct.tmpDir)
	ct.rec = rec
	ct.domainConn = fake.NewFakeDomainConnection(ct.rec.Child("domain conn"))
	ct.storageConn = fake.NewFakeStorageConnection(ct.rec.Child("storage"))

	ct.metadataStore, err = metadata.NewFakeMetadataStore()
	if err != nil {
		t.Fatalf("Failed to create fake bolt client: %v", err)
	}

	imageTool, err := NewImageTool(ct.storageConn, downloader, "default")
	if err != nil {
		t.Fatalf("Failed to create ImageTool: %v", err)
	}

	volSrc := CombineVMVolumeSources(
		GetRootVolume,
		ScanFlexvolumes,
		// XXX: GetNocloudVolume must go last because it
		// doesn't produce correct name for cdrom devices
		GetNocloudVolume)
	ct.virtTool, err = NewVirtualizationTool(ct.domainConn, ct.storageConn, imageTool, ct.metadataStore, "volumes", "loop*", volSrc)
	if err != nil {
		t.Fatalf("failed to create VirtualizationTool: %v", err)
	}
	ct.virtTool.SetClock(ct.clock)
	// avoid unneeded difs in the golden master data
	ct.virtTool.SetForceKVM(true)
	ct.kubeletRootDir = filepath.Join(ct.tmpDir, "kubelet-root")
	ct.virtTool.SetKubeletRootDir(ct.kubeletRootDir)

	// TODO: move image metadata store & name conversion to ImageTool
	// (i.e. methods like RemoveImage should accept image name)
	imageVolumeName, err := ImageNameToVolumeName(fakeImageName)
	if err != nil {
		t.Fatalf("Error getting volume name for image %q: %v", fakeImageName, err)
	}

	if _, err := imageTool.PullRemoteImageToVolume(fakeImageName, imageVolumeName, imagetranslation.NewImageNameTranslator()); err != nil {
		t.Fatalf("Error pulling image %q to volume %q: %v", fakeImageName, imageVolumeName, err)
	}

	return ct
}

func (ct *containerTester) setPodSandbox(config *kubeapi.PodSandboxConfig) {
	psi, _ := metadata.NewPodSandboxInfo(config, fakeCNIConfig, kubeapi.PodSandboxState_SANDBOX_READY, ct.clock)
	sandbox := ct.metadataStore.PodSandbox(config.Metadata.Uid)
	err := sandbox.Save(
		func(c *metadata.PodSandboxInfo) (*metadata.PodSandboxInfo, error) {
			return psi, nil
		},
	)
	if err != nil {
		ct.t.Fatalf("Failed to store pod sandbox: %v", err)
	}
}

func (ct *containerTester) teardown() {
	os.RemoveAll(ct.tmpDir)
}

func (ct *containerTester) createContainer(sandbox *kubeapi.PodSandboxConfig, mounts []*kubeapi.Mount) string {
	req := &kubeapi.CreateContainerRequest{
		PodSandboxId: sandbox.Metadata.Uid,
		Config: &kubeapi.ContainerConfig{
			Metadata: &kubeapi.ContainerMetadata{
				Name:    fakeContainerName,
				Attempt: fakeContainerAttempt,
			},
			Image: &kubeapi.ImageSpec{
				Image: fakeImageName,
			},
			Mounts:      mounts,
			Annotations: map[string]string{"foo": "bar"},
		},
		SandboxConfig: sandbox,
	}
	// Here we pass "" as cniConfig argument of GetVMConfig because we
	// don't test CNI aspect here. It's taken care of in pkg/manager
	// and cloud-init part of this package.
	vmConfig, err := GetVMConfig(req, "")
	if err != nil {
		ct.t.Fatalf("GetVMConfig(): %v", err)
	}
	containerId, err := ct.virtTool.CreateContainer(vmConfig, "/tmp/fakenetns")
	if err != nil {
		ct.t.Fatalf("CreateContainer: %v", err)
	}
	return containerId
}

func (ct *containerTester) listContainers(filter *kubeapi.ContainerFilter) []*kubeapi.Container {
	containers, err := ct.virtTool.ListContainers(nil)
	if err != nil {
		ct.t.Fatalf("ListContainers() failed: %v", err)
	}
	return containers
}

func (ct *containerTester) containerStatus(containerId string) *kubeapi.ContainerStatus {
	status, err := ct.virtTool.ContainerStatus(containerId)
	if err != nil {
		ct.t.Errorf("ContainerStatus(): %v", err)
	}
	return status
}

func (ct *containerTester) startContainer(containerId string) {
	if err := ct.virtTool.StartContainer(containerId); err != nil {
		ct.t.Fatalf("StartContainer failed for container %q: %v", containerId, err)
	}
}

func (ct *containerTester) stopContainer(containerId string) {
	if err := ct.virtTool.StopContainer(containerId, stopContainerTimeout); err != nil {
		ct.t.Fatalf("StopContainer failed for container %q: %v", containerId, err)
	}
}

func (ct *containerTester) removeContainer(containerId string) {
	if err := ct.virtTool.RemoveContainer(containerId); err != nil {
		ct.t.Fatalf("RemoveContainer failed for container %q: %v", containerId, err)
	}
}

func (ct *containerTester) verifyContainerRootfsExists(container *kubeapi.Container) bool {
	storagePool, err := ct.storageConn.LookupStoragePoolByName("volumes")
	if err != nil {
		ct.t.Fatal("Expected to found 'volumes' storage pool but failed with: %v", err)
	}
	// TODO: this is third place where rootfs volume name is calculated
	// so there should be a func which will do it in consistent way there,
	// in virtlet_root_volumesource.go and in virtualization.go
	_, err = storagePool.LookupVolumeByName("virtlet_root_" + container.PodSandboxId)
	return err == nil
}

func TestContainerLifecycle(t *testing.T) {
	ct := newContainerTester(t, fake.NewToplevelRecorder())
	defer ct.teardown()

	sandbox := criapi.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)

	containers := ct.listContainers(nil)
	if len(containers) != 0 {
		t.Errorf("Unexpected containers when no containers are started: %#v", containers)
	}

	containerId := ct.createContainer(sandbox, nil)

	containers = ct.listContainers(nil)
	if len(containers) != 1 {
		t.Errorf("Expected single container to be started, but got: %#v", containers)
	}
	container := containers[0]
	if container.Id != containerId {
		t.Errorf("Bad container id in response: %q instead of %q", containers[0].Id, containerId)
	}
	if container.State != kubeapi.ContainerState_CONTAINER_CREATED {
		t.Errorf("Bad container state: %v instead of %v", containers[0].State, kubeapi.ContainerState_CONTAINER_CREATED)
	}
	if container.Metadata.Name != fakeContainerName {
		t.Errorf("Bad container name: %q instead of %q", containers[0].Metadata.Name, fakeContainerName)
	}
	if container.Metadata.Attempt != fakeContainerAttempt {
		t.Errorf("Bad container attempt: %d instead of %d", containers[0].Metadata.Attempt, fakeContainerAttempt)
	}
	if container.Labels[kubetypes.KubernetesContainerNameLabel] != fakeContainerName {
		t.Errorf("Bad container name label: %q instead of %q", containers[0].Labels[kubetypes.KubernetesContainerNameLabel], fakeContainerName)
	}
	if container.Annotations["foo"] != "bar" {
		t.Errorf("Bad container annotation value: %q instead of %q", containers[0].Annotations["foo"], "bar")
	}
	ct.rec.Rec("container list after the container is created", containers)

	ct.clock.Advance(1 * time.Second)
	ct.startContainer(containerId)

	status := ct.containerStatus(containerId)
	if status.State != kubeapi.ContainerState_CONTAINER_RUNNING {
		t.Errorf("Bad container state: %v instead of %v", status.State, kubeapi.ContainerState_CONTAINER_RUNNING)
	}
	ct.rec.Rec("container status after the container is started", status)

	ct.stopContainer(containerId)

	status = ct.containerStatus(containerId)
	if status.State != kubeapi.ContainerState_CONTAINER_EXITED {
		t.Errorf("Bad container state: %v instead of %v", status.State, kubeapi.ContainerState_CONTAINER_EXITED)
	}
	if status.Metadata.Name != fakeContainerName {
		t.Errorf("Bad container name: %q instead of %q", status.Metadata.Name, fakeContainerName)
	}
	if status.Metadata.Attempt != fakeContainerAttempt {
		t.Errorf("Bad container attempt: %d instead of %d", status.Metadata.Attempt, fakeContainerAttempt)
	}
	if status.Labels[kubetypes.KubernetesContainerNameLabel] != fakeContainerName {
		t.Errorf("Bad container name label: %q instead of %q", containers[0].Labels[kubetypes.KubernetesContainerNameLabel], fakeContainerName)
	}
	if status.Annotations["foo"] != "bar" {
		t.Errorf("Bad container annotation value: %q instead of %q", status.Annotations["foo"], "bar")
	}
	ct.rec.Rec("container status after the container is stopped", status)

	ct.removeContainer(containerId)

	containers = ct.listContainers(nil)
	if len(containers) != 0 {
		t.Errorf("Unexpected containers when no containers are started: %#v", containers)
	}

	if ct.verifyContainerRootfsExists(container) {
		t.Errorf("Rootfs volume was not deleted for the container: %#v", container)
	}

	gm.Verify(t, ct.rec.Content())
}

func TestDomainForcedShutdown(t *testing.T) {
	ct := newContainerTester(t, fake.NewToplevelRecorder())
	defer ct.teardown()

	sandbox := criapi.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)

	containerId := ct.createContainer(sandbox, nil)
	ct.clock.Advance(1 * time.Second)
	ct.startContainer(containerId)

	ct.domainConn.SetIgnoreShutdown(true)
	go func() {
		// record a couple of ignored shutdown attempts before container destruction
		ct.clock.BlockUntil(1)
		ct.clock.Advance(6 * time.Second)
		ct.clock.BlockUntil(1)
		ct.clock.Advance(6 * time.Second)
		ct.clock.BlockUntil(1)
		ct.clock.Advance(30 * time.Second)
	}()

	ct.rec.Rec("invoking StopContainer()", nil)
	ct.stopContainer(containerId)
	status := ct.containerStatus(containerId)
	if status.State != kubeapi.ContainerState_CONTAINER_EXITED {
		t.Errorf("Bad container state: %v instead of %v", status.State, kubeapi.ContainerState_CONTAINER_EXITED)
	}
	ct.rec.Rec("container status after the container is stopped", status)

	ct.rec.Rec("invoking RemoveContainer()", nil)
	ct.removeContainer(containerId)
	gm.Verify(t, ct.rec.Content())
}

func TestDoubleStartError(t *testing.T) {
	ct := newContainerTester(t, fake.NewToplevelRecorder())
	defer ct.teardown()

	sandbox := criapi.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)

	containerId := ct.createContainer(sandbox, nil)
	ct.clock.Advance(1 * time.Second)
	ct.startContainer(containerId)
	if err := ct.virtTool.StartContainer(containerId); err == nil {
		t.Errorf("2nd StartContainer() didn't produce an error")
	}
}

type volMount struct {
	name          string
	containerPath string
}

func TestDomainDefinitions(t *testing.T) {
	flexVolumeDriver := flexvolume.NewFlexVolumeDriver(func() string {
		// note that this is only good for just one flexvolume
		return fakeUuid
	}, flexvolume.NullMounter)
	for _, tc := range []struct {
		name        string
		annotations map[string]string
		flexVolumes map[string]map[string]interface{}
		mounts      []volMount
	}{
		{
			name: "plain domain",
		},
		{
			name: "raw devices",
			flexVolumes: map[string]map[string]interface{}{
				"raw": map[string]interface{}{
					"type": "raw",
					// FIXME: here we depend upon the fact that /dev/loop0
					// indeed exists in the build container. But we shouldn't.
					"path": "/dev/loop0",
				},
			},
		},
		{
			name: "volumes",
			flexVolumes: map[string]map[string]interface{}{
				"vol1": map[string]interface{}{
					"type": "qcow2",
				},
				"vol2": map[string]interface{}{
					"type":     "qcow2",
					"capacity": "2MB",
				},
				"vol3": map[string]interface{}{
					"type": "qcow2",
				},
			},
		},
		{
			name: "vcpu count",
			annotations: map[string]string{
				"VirtletVCPUCount": "4",
			},
		},
		{
			name: "ceph flexvolume",
			flexVolumes: map[string]map[string]interface{}{
				"ceph": map[string]interface{}{
					"type":    "ceph",
					"monitor": "127.0.0.1:6789",
					"pool":    "libvirt-pool",
					"volume":  "rbd-test-image",
					"secret":  "Zm9vYmFyCg==",
					"user":    "libvirt",
				},
			},
			mounts: []volMount{
				{
					name:          "ceph",
					containerPath: "/var/lib/whatever",
				},
			},
		},
		{
			name: "cloud-init",
			annotations: map[string]string{
				"VirtletSSHKeys": "key1\nkey2",
			},
		},
		{
			name: "cloud-init with user data",
			annotations: map[string]string{
				"VirtletSSHKeys": "key1\nkey2",
				"VirtletCloudInitUserData": `
                                  users:
                                  - name: cloudy`,
			},
		},
		{
			name: "virtio disk driver",
			annotations: map[string]string{
				"VirtletDiskDriver": "virtio",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := fake.NewToplevelRecorder()

			ct := newContainerTester(t, rec)
			defer ct.teardown()

			sandbox := criapi.GetSandboxes(1)[0]
			sandbox.Annotations = tc.annotations
			ct.setPodSandbox(sandbox)

			for name, def := range tc.flexVolumes {
				targetDir := filepath.Join(ct.kubeletRootDir, sandbox.Metadata.Uid, "volumes/virtlet~flexvolume_driver", name)
				resultStr := flexVolumeDriver.Run([]string{"mount", targetDir, utils.MapToJson(def)})
				var r map[string]interface{}
				if err := json.Unmarshal([]byte(resultStr), &r); err != nil {
					t.Errorf("failed to unmarshal flexvolume definition")
					continue
				}
				if r["status"] != "Success" {
					t.Errorf("mounting flexvolume %q failed: %s", name, r["message"])
				}
			}

			var mounts []*kubeapi.Mount
			for _, m := range tc.mounts {
				mounts = append(mounts, &kubeapi.Mount{
					HostPath:      filepath.Join(ct.kubeletRootDir, sandbox.Metadata.Uid, "volumes/virtlet~flexvolume_driver", m.name),
					ContainerPath: m.containerPath,
				})
			}
			containerId := ct.createContainer(sandbox, mounts)
			ct.removeContainer(containerId)
			gm.Verify(t, ct.rec.Content())
		})
	}
}

func TestDomainResourceConstraints(t *testing.T) {
	cpuQuota := 25000
	cpuPeriod := 100000
	cpuShares := 100
	memoryLimit := 1234567
	cpuCount := 2

	rec := fake.NewToplevelRecorder()
	rec.AddFilter("DefineDomain")
	ct := newContainerTester(t, rec)
	defer ct.teardown()
	sandbox := criapi.GetSandboxes(1)[0]
	sandbox.Annotations = map[string]string{
		"VirtletVCPUCount": strconv.Itoa(cpuCount),
	}
	ct.setPodSandbox(sandbox)
	req := &kubeapi.CreateContainerRequest{
		PodSandboxId: sandbox.Metadata.Uid,
		Config: &kubeapi.ContainerConfig{
			Metadata: &kubeapi.ContainerMetadata{
				Name:    fakeContainerName,
				Attempt: fakeContainerAttempt,
			},
			Image: &kubeapi.ImageSpec{
				Image: fakeImageName,
			},
			Linux: &kubeapi.LinuxContainerConfig{
				Resources: &kubeapi.LinuxContainerResources{
					CpuQuota:           int64(cpuQuota),
					CpuPeriod:          int64(cpuPeriod),
					CpuShares:          int64(cpuShares),
					MemoryLimitInBytes: int64(memoryLimit),
				},
			},
		},
		SandboxConfig: sandbox,
	}
	vmConfig, err := GetVMConfig(req, "")
	if err != nil {
		t.Fatalf("GetVMConfig(): %v", err)
	}
	_, err = ct.virtTool.CreateContainer(vmConfig, "/tmp/fakenetns")
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}

	domain := rec.Content()[0].Data.(*libvirtxml.Domain)

	if domain.VCPU == nil {
		t.Error("vCPU is not set")
	} else if domain.VCPU.Value != cpuCount {
		t.Errorf("unexpected vCPU count value: expected %v, got %v", cpuCount, domain.VCPU.Value)
	}

	if domain.CPUTune == nil {
		t.Error("CPUTune is not set")
	} else {
		expectedQuota := int64(cpuQuota / cpuCount)
		if domain.CPUTune.Quota == nil {
			t.Error("CPU quota is not set")
		} else if domain.CPUTune.Quota.Value != expectedQuota {
			t.Errorf("unexpected CPU quota value: expected %v, got %v", expectedQuota, domain.CPUTune.Quota.Value)
		}

		if domain.CPUTune.Shares == nil {
			t.Error("CPU shares is not set")
		} else if domain.CPUTune.Shares.Value != uint(cpuShares) {
			t.Errorf("unexpected CPU shares value: expected %v, got %v", cpuShares, domain.CPUTune.Shares.Value)
		}

		if domain.CPUTune.Period == nil {
			t.Error("CPU period is not set")
		} else if domain.CPUTune.Period.Value != uint64(cpuPeriod) {
			t.Errorf("unexpected CPU period value: expected %v, got %v", cpuShares, domain.CPUTune.Period.Value)
		}
	}

	if domain.Memory == nil {
		t.Error("Memory is not set")
	} else if domain.Memory.Value != uint(memoryLimit) || domain.Memory.Unit != "b" {
		t.Errorf("unexpected memory limitvalue: expected %vb, got %v%s", memoryLimit, domain.Memory.Value, domain.Memory.Unit)
	}
}
