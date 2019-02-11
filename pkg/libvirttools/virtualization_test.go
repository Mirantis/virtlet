/*
Copyright 2016-2019 Mirantis

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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/Mirantis/virtlet/pkg/flexvolume"
	"github.com/Mirantis/virtlet/pkg/fs"
	fakefs "github.com/Mirantis/virtlet/pkg/fs/fake"
	"github.com/Mirantis/virtlet/pkg/metadata"
	fakemeta "github.com/Mirantis/virtlet/pkg/metadata/fake"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/utils"
	fakeutils "github.com/Mirantis/virtlet/pkg/utils/fake"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/gm"
)

const (
	fakeImageName        = "fake/image1"
	fakeCNIConfig        = `{"noCniForNow":true}`
	fakeUUID             = "abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"
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
	rec            *testutils.TopLevelRecorder
	domainConn     *fake.FakeDomainConnection
	storageConn    *fake.FakeStorageConnection
	metadataStore  metadata.Store
}

func newContainerTester(t *testing.T, rec *testutils.TopLevelRecorder, cmds []fakeutils.CmdSpec, files map[string]string) *containerTester {
	ct := &containerTester{
		t:     t,
		clock: clockwork.NewFakeClockAt(time.Date(2017, 5, 30, 20, 19, 0, 0, time.UTC)),
	}

	var err error
	ct.tmpDir, err = ioutil.TempDir("", "virtualization-test-")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}

	// __config__  is a hint for fake libvirt domain to fix the path so it becomes non-volatile
	SetConfigIsoDir(filepath.Join(ct.tmpDir, "__config__"))

	ct.rec = rec
	ct.domainConn = fake.NewFakeDomainConnection(ct.rec.Child("domain conn"))
	ct.storageConn = fake.NewFakeStorageConnection(ct.rec.Child("storage"))

	ct.metadataStore, err = metadata.NewFakeStore()
	if err != nil {
		t.Fatalf("Failed to create fake bolt client: %v", err)
	}

	imageManager := newFakeImageManager(ct.rec)
	ct.kubeletRootDir = filepath.Join(ct.tmpDir, "__fs__/kubelet-root")
	mountDir := filepath.Join(ct.tmpDir, "__fs__/mounts")
	virtConfig := VirtualizationConfig{
		VolumePoolName:       "volumes",
		RawDevices:           []string{"loop*"},
		KubeletRootDir:       ct.kubeletRootDir,
		StreamerSocketPath:   "/var/lib/libvirt/streamer.sock",
		SharedFilesystemPath: mountDir,
	}
	fakeCommander := fakeutils.NewCommander(rec, cmds)
	fakeCommander.ReplaceTempPath("__pods__", "/fakedev")

	fs := fakefs.NewFakeFileSystem(t, rec, mountDir, files)

	ct.virtTool = NewVirtualizationTool(
		ct.domainConn, ct.storageConn, imageManager, ct.metadataStore,
		GetDefaultVolumeSource(), virtConfig, fs,
		fakeCommander)
	ct.virtTool.SetClock(ct.clock)

	return ct
}

func (ct *containerTester) setPodSandbox(config *types.PodSandboxConfig) {
	psi, _ := metadata.NewPodSandboxInfo(config, nil, types.PodSandboxState_SANDBOX_READY, ct.clock)
	sandbox := ct.metadataStore.PodSandbox(config.Uid)
	err := sandbox.Save(func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
		return psi, nil
	})
	if err != nil {
		ct.t.Fatalf("Failed to store pod sandbox: %v", err)
	}
}

func (ct *containerTester) teardown() {
	os.RemoveAll(ct.tmpDir)
}

func (ct *containerTester) createContainer(sandbox *types.PodSandboxConfig, mounts []types.VMMount, volDevs []types.VMVolumeDevice) string {
	vmConfig := &types.VMConfig{
		PodSandboxID:         sandbox.Uid,
		PodName:              sandbox.Name,
		PodNamespace:         sandbox.Namespace,
		Name:                 fakeContainerName,
		Image:                fakeImageName,
		Attempt:              fakeContainerAttempt,
		PodAnnotations:       sandbox.Annotations,
		ContainerAnnotations: map[string]string{"foo": "bar"},
		Mounts:               mounts,
		VolumeDevices:        volDevs,
		LogDirectory:         fmt.Sprintf("/var/log/pods/%s", sandbox.Uid),
		LogPath:              fmt.Sprintf("%s_%d.log", fakeContainerName, fakeContainerAttempt),
	}
	containerID, err := ct.virtTool.CreateContainer(vmConfig, "/tmp/fakenetns")
	if err != nil {
		ct.t.Fatalf("CreateContainer: %v", err)
	}
	return containerID
}

func (ct *containerTester) listContainers(filter *types.ContainerFilter) []*types.ContainerInfo {
	containers, err := ct.virtTool.ListContainers(filter)
	if err != nil {
		ct.t.Fatalf("ListContainers() failed: %v", err)
	}
	return containers
}

func (ct *containerTester) containerInfo(containerID string) *types.ContainerInfo {
	status, err := ct.virtTool.ContainerInfo(containerID)
	if err != nil {
		ct.t.Errorf("ContainerInfo(): %v", err)
	}
	return status
}

func (ct *containerTester) startContainer(containerID string) {
	if err := ct.virtTool.StartContainer(containerID); err != nil {
		ct.t.Fatalf("StartContainer failed for container %q: %v", containerID, err)
	}
}

func (ct *containerTester) stopContainer(containerID string) {
	if err := ct.virtTool.StopContainer(containerID, stopContainerTimeout); err != nil {
		ct.t.Fatalf("StopContainer failed for container %q: %v", containerID, err)
	}
}

func (ct *containerTester) removeContainer(containerID string) {
	if err := ct.virtTool.RemoveContainer(containerID); err != nil {
		ct.t.Fatalf("RemoveContainer failed for container %q: %v", containerID, err)
	}
}

func (ct *containerTester) verifyContainerRootfsExists(containerInfo *types.ContainerInfo) bool {
	storagePool, err := ct.storageConn.LookupStoragePoolByName("volumes")
	if err != nil {
		ct.t.Fatalf("Expected to found 'volumes' storage pool but failed with: %v", err)
	}
	// TODO: this is third place where rootfs volume name is calculated
	// so there should be a func which will do it in consistent way there,
	// in virtlet_root_volumesource.go and in virtualization.go
	_, err = storagePool.LookupVolumeByName("virtlet_root_" + containerInfo.Config.PodSandboxID)
	return err == nil
}

func TestContainerLifecycle(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil, nil)
	defer ct.teardown()

	sandbox := fakemeta.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)

	containers := ct.listContainers(nil)
	if len(containers) != 0 {
		t.Errorf("Unexpected containers when no containers are started: %#v", containers)
	}

	containerID := ct.createContainer(sandbox, nil, nil)

	containers = ct.listContainers(nil)
	if len(containers) != 1 {
		t.Errorf("Expected single container to be started, but got: %#v", containers)
	}
	container := containers[0]
	if container.Id != containerID {
		t.Errorf("Bad container id in response: %q instead of %q", containers[0].Id, containerID)
	}
	if container.State != types.ContainerState_CONTAINER_CREATED {
		t.Errorf("Bad container state: %v instead of %v", containers[0].State, types.ContainerState_CONTAINER_CREATED)
	}
	if container.Config.Name != fakeContainerName {
		t.Errorf("Bad container name: %q instead of %q", containers[0].Config.Name, fakeContainerName)
	}
	if container.Config.Attempt != fakeContainerAttempt {
		t.Errorf("Bad container attempt: %d instead of %d", containers[0].Config.Attempt, fakeContainerAttempt)
	}
	if container.Config.ContainerLabels[KubernetesContainerNameLabel] != fakeContainerName {
		t.Errorf("Bad container name label: %q instead of %q", containers[0].Config.ContainerLabels[KubernetesContainerNameLabel], fakeContainerName)
	}
	if container.Config.ContainerAnnotations["foo"] != "bar" {
		t.Errorf("Bad container annotation value: %q instead of %q", containers[0].Config.ContainerAnnotations["foo"], "bar")
	}
	ct.rec.Rec("container list after the container is created", containers)

	ct.clock.Advance(1 * time.Second)
	ct.startContainer(containerID)

	container = ct.containerInfo(containerID)
	if container.State != types.ContainerState_CONTAINER_RUNNING {
		t.Errorf("Bad container state: %v instead of %v", container.State, types.ContainerState_CONTAINER_RUNNING)
	}
	ct.rec.Rec("container info after the container is started", container)

	ct.stopContainer(containerID)

	container = ct.containerInfo(containerID)
	if container.State != types.ContainerState_CONTAINER_EXITED {
		t.Errorf("Bad container state: %v instead of %v", container.State, types.ContainerState_CONTAINER_EXITED)
	}
	if container.Config.Name != fakeContainerName {
		t.Errorf("Bad container name: %q instead of %q", container.Config.Name, fakeContainerName)
	}
	if container.Config.Attempt != fakeContainerAttempt {
		t.Errorf("Bad container attempt: %d instead of %d", container.Config.Attempt, fakeContainerAttempt)
	}
	if container.Config.ContainerLabels[KubernetesContainerNameLabel] != fakeContainerName {
		t.Errorf("Bad container name label: %q instead of %q", containers[0].Config.ContainerLabels[KubernetesContainerNameLabel], fakeContainerName)
	}
	if container.Config.ContainerAnnotations["foo"] != "bar" {
		t.Errorf("Bad container annotation value: %q instead of %q", container.Config.ContainerAnnotations["foo"], "bar")
	}
	ct.rec.Rec("container info after the container is stopped", container)

	ct.removeContainer(containerID)

	containers = ct.listContainers(nil)
	if len(containers) != 0 {
		t.Errorf("Unexpected containers when no containers are started: %#v", containers)
	}

	if ct.verifyContainerRootfsExists(container) {
		t.Errorf("Rootfs volume was not deleted for the container: %#v", container)
	}

	gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
}

func TestDomainForcedShutdown(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil, nil)
	defer ct.teardown()

	sandbox := fakemeta.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)

	containerID := ct.createContainer(sandbox, nil, nil)
	ct.clock.Advance(1 * time.Second)
	ct.startContainer(containerID)

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
	ct.stopContainer(containerID)
	container := ct.containerInfo(containerID)
	if container.State != types.ContainerState_CONTAINER_EXITED {
		t.Errorf("Bad container state: %v instead of %v", container.State, types.ContainerState_CONTAINER_EXITED)
	}
	ct.rec.Rec("container info after the container is stopped", container)

	ct.rec.Rec("invoking RemoveContainer()", nil)
	ct.removeContainer(containerID)
	gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
}

func TestDoubleStartError(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil, nil)
	defer ct.teardown()

	sandbox := fakemeta.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)

	containerID := ct.createContainer(sandbox, nil, nil)
	ct.clock.Advance(1 * time.Second)
	ct.startContainer(containerID)
	if err := ct.virtTool.StartContainer(containerID); err == nil {
		t.Errorf("2nd StartContainer() didn't produce an error")
	}
}

type volMount struct {
	name          string
	containerPath string
	podSubpath    string
}

type volDevice struct {
	name       string
	devicePath string
	size       int
}

func TestDomainDefinitions(t *testing.T) {
	flexVolumeDriver := flexvolume.NewDriver(func() string {
		// note that this is only good for just one flexvolume
		return fakeUUID
	}, fs.NullFileSystem)
	for _, tc := range []struct {
		name        string
		annotations map[string]string
		flexVolumes map[string]map[string]interface{}
		mounts      []volMount
		volDevs     []volDevice
		cmds        []fakeutils.CmdSpec
	}{
		{
			name: "plain domain",
		},
		{
			name: "system UUID",
			annotations: map[string]string{
				"VirtletSystemUUID": "53008994-44c0-4017-ad44-9c49758083da",
			},
		},
		{
			name: "raw devices",
			flexVolumes: map[string]map[string]interface{}{
				"raw": {
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
				"vol1": {
					"type": "qcow2",
				},
				"vol2": {
					"type":     "qcow2",
					"capacity": "2MB",
				},
				"vol3": {
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
				"ceph": {
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
					podSubpath:    "volumes/virtlet~flexvolume_driver",
				},
			},
		},
		{
			name: "raw block volume",
			volDevs: []volDevice{
				{
					name:       "testdev",
					devicePath: "/dev/tst",
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
		{
			name: "persistent rootfs",
			volDevs: []volDevice{
				{
					name:       "root",
					devicePath: "/",
					size:       512000,
				},
			},
			cmds: []fakeutils.CmdSpec{
				{
					Match:  "blockdev --getsz",
					Stdout: "1000",
				},
				{
					Match: "qemu-img convert",
				},
				{
					Match: "dmsetup create",
				},
				{
					Match: "dmsetup remove",
				},
			},
		},
		{
			name: "9pfs volume",
			mounts: []volMount{
				{
					name:          "9pfs-vol",
					containerPath: "/var/lib/foobar",
					podSubpath:    "volumes/kubernetes.io~rbd",
				},
			},
		},
		// TODO: add test cases for rootfs / persistent rootfs file injection
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := testutils.NewToplevelRecorder()

			ct := newContainerTester(t, rec, tc.cmds, nil)
			defer ct.teardown()

			sandbox := fakemeta.GetSandboxes(1)[0]
			sandbox.Annotations = tc.annotations
			ct.setPodSandbox(sandbox)

			for name, def := range tc.flexVolumes {
				targetDir := filepath.Join(ct.kubeletRootDir, sandbox.Uid, "volumes/virtlet~flexvolume_driver", name)
				resultStr := flexVolumeDriver.Run([]string{"mount", targetDir, utils.ToJSON(def)})
				var r map[string]interface{}
				if err := json.Unmarshal([]byte(resultStr), &r); err != nil {
					t.Errorf("failed to unmarshal flexvolume definition")
					continue
				}
				if r["status"] != "Success" {
					t.Errorf("mounting flexvolume %q failed: %s", name, r["message"])
				}
			}

			var mounts []types.VMMount
			for _, m := range tc.mounts {
				mounts = append(mounts, types.VMMount{
					HostPath:      filepath.Join(ct.kubeletRootDir, sandbox.Uid, m.podSubpath, m.name),
					ContainerPath: m.containerPath,
				})
			}

			var volDevs []types.VMVolumeDevice
			for _, d := range tc.volDevs {
				// __pods__  is a hint for fake libvirt domain to fix the path so it becomes non-volatile
				baseDir := filepath.Join(ct.kubeletRootDir, "__pods__", sandbox.Uid, "volumeDevices/kubernetes.io~local-volume")
				if err := os.MkdirAll(baseDir, 0777); err != nil {
					t.Fatal(err)
				}
				hostPath := filepath.Join(baseDir, d.name)
				if f, err := os.Create(hostPath); err != nil {
					t.Fatal(err)
				} else {
					if d.size != 0 {
						if _, err := f.Write(make([]byte, d.size)); err != nil {
							t.Fatal(err)
						}
					}
					if err := f.Close(); err != nil {
						t.Fatal(err)
					}
				}
				volDevs = append(volDevs, types.VMVolumeDevice{
					DevicePath: d.devicePath,
					HostPath:   hostPath,
				})
			}

			containerID := ct.createContainer(sandbox, mounts, volDevs)

			// startContainer will cause fake Domain
			// to dump the cloudinit iso content
			ct.startContainer(containerID)
			ct.removeContainer(containerID)
			gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
		})
	}
}

func TestDomainResourceConstraints(t *testing.T) {
	cpuQuota := 25000
	cpuPeriod := 100000
	cpuShares := 100
	memoryLimit := 1234567
	cpuCount := 2

	rec := testutils.NewToplevelRecorder()
	rec.AddFilter("DefineDomain")
	ct := newContainerTester(t, rec, nil, nil)
	defer ct.teardown()
	sandbox := fakemeta.GetSandboxes(1)[0]
	sandbox.Annotations = map[string]string{
		"VirtletVCPUCount": strconv.Itoa(cpuCount),
	}
	ct.setPodSandbox(sandbox)
	vmConfig := &types.VMConfig{
		PodSandboxID:       sandbox.Uid,
		PodName:            sandbox.Name,
		PodNamespace:       sandbox.Namespace,
		Name:               fakeContainerName,
		Image:              fakeImageName,
		Attempt:            fakeContainerAttempt,
		MemoryLimitInBytes: int64(memoryLimit),
		CPUShares:          int64(cpuShares),
		CPUPeriod:          int64(cpuPeriod),
		CPUQuota:           int64(cpuQuota),
		PodAnnotations:     sandbox.Annotations,
		LogDirectory:       fmt.Sprintf("/var/log/pods/%s", sandbox.Uid),
		LogPath:            fmt.Sprintf("%s_%d.log", fakeContainerName, fakeContainerAttempt),
	}
	if _, err := ct.virtTool.CreateContainer(vmConfig, "/tmp/fakenetns"); err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}

	gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
}
