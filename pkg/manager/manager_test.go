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

package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/jonboulle/clockwork"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/flexvolume"
	"github.com/Mirantis/virtlet/pkg/image"
	fakeimage "github.com/Mirantis/virtlet/pkg/image/fake"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	fakevirt "github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/criapi"
	"github.com/Mirantis/virtlet/tests/gm"
)

const (
	podTimestap = 1524648266720331175
)

type fakeFDManager struct {
	rec         testutils.Recorder
	items       map[string]bool
	lastIpOctet byte
}

var _ tapmanager.FDManager = &fakeFDManager{}

func newFakeFDManager(rec testutils.Recorder) *fakeFDManager {
	return &fakeFDManager{
		rec:         rec,
		items:       make(map[string]bool),
		lastIpOctet: 5,
	}
}

func (m *fakeFDManager) AddFDs(key string, data interface{}) ([]byte, error) {
	m.rec.Rec("AddFDs", map[string]interface{}{
		"key":  key,
		"data": data,
	})

	if m.items[key] {
		return nil, fmt.Errorf("Duplicate key: %q", key)
	}

	fdPayload := data.(*tapmanager.GetFDPayload)
	if fdPayload.Description == nil {
		return nil, fmt.Errorf("AddFDs(): bad data: %#v", data)
	}
	macAddr := "42:a4:a6:22:80:2e"
	parsedMacAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		log.Panicf("Error parsing hwaddr %q: %v", macAddr, err)
	}
	nsPath := cni.PodNetNSPath(fdPayload.Description.PodID)
	csn := &network.ContainerSideNetwork{
		Result: &cnicurrent.Result{
			Interfaces: []*cnicurrent.Interface{
				{
					Name:    "eth0",
					Mac:     macAddr,
					Sandbox: nsPath,
				},
			},
			IPs: []*cnicurrent.IPConfig{
				{
					Version:   "4",
					Interface: 0,
					Address: net.IPNet{
						IP:   net.IP{10, 1, 90, m.lastIpOctet},
						Mask: net.IPMask{255, 255, 255, 0},
					},
					Gateway: net.IP{10, 1, 90, 1},
				},
			},
			Routes: []*cnitypes.Route{
				{
					Dst: net.IPNet{
						IP:   net.IP{0, 0, 0, 0},
						Mask: net.IPMask{0, 0, 0, 0},
					},
					GW: net.IP{10, 1, 90, 1},
				},
			},
		},
		NsPath: nsPath,
		Interfaces: []*network.InterfaceDescription{
			{
				Type:         network.InterfaceTypeTap,
				HardwareAddr: parsedMacAddr,
			},
		},
	}

	respData, err := json.Marshal(csn)
	if err != nil {
		return nil, fmt.Errorf("error marshalling net config: %v", err)
	}

	m.lastIpOctet++
	m.items[key] = true
	return respData, nil
}

func (m *fakeFDManager) ReleaseFDs(key string) error {
	m.rec.Rec("ReleaseFDs", key)
	if !m.items[key] {
		return fmt.Errorf("key not found: %q", key)
	}
	return nil
}

func (m *fakeFDManager) Recover(key string, data interface{}) error {
	m.rec.Rec("Recover", key)
	if m.items[key] {
		return fmt.Errorf("Duplicate key: %q", key)
	}
	return nil
}

func TestPodSanboxConfigValidation(t *testing.T) {
	invalidSandboxes := criapi.GetSandboxes(1)

	// Now let's make generated configs to be invalid
	invalidSandboxes[0].Metadata = nil

	if err := validatePodSandboxConfig(invalidSandboxes[0]); err == nil {
		t.Errorf("Invalid pod sandbox passed validation:\n%s", spew.Sdump(invalidSandboxes[0]))
	}
}

func translateImageName(ctx context.Context, name string) image.Endpoint {
	return image.Endpoint{URL: name, MaxRedirects: -1}
}

type virtletManagerTester struct {
	t              *testing.T
	rec            *testutils.TopLevelRecorder
	manager        *VirtletManager
	tmpDir         string
	kubeletRootDir string
}

func makeVirtletManagerTester(t *testing.T) *virtletManagerTester {
	rec := testutils.NewToplevelRecorder()
	tmpDir, err := ioutil.TempDir("", "virtualization-test-")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	// __config__  is a hint for fake libvirt domain to fix the path
	libvirttools.SetConfigIsoDir(filepath.Join(tmpDir, "__config__"))
	fdManager := newFakeFDManager(rec.Child("fdManager"))
	imageStore := fakeimage.NewFakeStore(rec.Child("imageStore"))
	metadataStore, err := metadata.NewFakeStore()
	if err != nil {
		t.Fatalf("Failed to create fake bolt client: %v", err)
	}
	domainConn := fakevirt.NewFakeDomainConnection(rec.Child("domain conn"))
	storageConn := fakevirt.NewFakeStorageConnection(rec.Child("storage"))
	clock := clockwork.NewFakeClockAt(time.Unix(0, podTimestap))
	virtTool := libvirttools.NewVirtualizationTool(domainConn, storageConn, imageStore, metadataStore, "volumes", "loop*", libvirttools.GetDefaultVolumeSource())
	virtTool.SetClock(clock)
	// avoid unneeded diffs in the golden master data
	virtTool.SetForceKVM(true)
	kubeletRootDir := filepath.Join(tmpDir, "kubelet-root")
	virtTool.SetKubeletRootDir(kubeletRootDir)
	manager := NewVirtletManager(virtTool, imageStore, metadataStore, fdManager, translateImageName)
	manager.clock = clock
	return &virtletManagerTester{
		t:              t,
		rec:            rec,
		manager:        manager,
		tmpDir:         tmpDir,
		kubeletRootDir: kubeletRootDir,
	}
}

func (tst *virtletManagerTester) teardown() {
	os.RemoveAll(tst.tmpDir)
}

func (tst *virtletManagerTester) invoke(name string, req interface{}) interface{} {
	tst.rec.Rec("enter: "+name, req)
	v := reflect.ValueOf(tst.manager)
	method := v.MethodByName(name)
	if method.Kind() == reflect.Invalid {
		tst.t.Fatalf("bad manager method %q", name)
	}
	ctx := context.Background()
	vals := method.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(req),
	})
	if len(vals) != 2 {
		tst.t.Fatalf("expected manager method %q to return 2 values but it returned %#v", name, vals)
	}
	if !vals[1].IsNil() {
		err, ok := vals[1].Interface().(error)
		if !ok {
			tst.t.Fatalf("2nd returned value is %#v instead of error", vals[1].Interface())
		}
		if err != nil {
			tst.t.Errorf("manager method %q returned error: %v", name, err)
		}
		return nil
	} else {
		resp := vals[0].Interface()
		tst.rec.Rec("leave: "+name, resp)
		return resp
	}
}

func (tst *virtletManagerTester) getSampleFlexvolMounts(podSandboxID string) []*kubeapi.Mount {
	flexVolumeDriver := flexvolume.NewFlexVolumeDriver(func() string {
		return "abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"
	}, flexvolume.NullMounter)
	flexVolDir := filepath.Join(tst.kubeletRootDir, podSandboxID, "volumes/virtlet~flexvolume_driver", "vol1")
	flexVolDef := map[string]interface{}{
		"type":     "qcow2",
		"capacity": "2MB",
	}
	resultStr := flexVolumeDriver.Run([]string{"mount", flexVolDir, utils.MapToJSON(flexVolDef)})
	var r map[string]interface{}
	if err := json.Unmarshal([]byte(resultStr), &r); err != nil {
		tst.t.Fatalf("failed to unmarshal flexvolume definition")
	}
	if r["status"] != "Success" {
		tst.t.Fatalf("mounting flexvolume vol1 failed: %s", r["message"])
	}
	return []*kubeapi.Mount{
		{
			ContainerPath: "/mnt",
			HostPath:      flexVolDir,
		},
	}
}

func (tst *virtletManagerTester) verify() {
	verifier := gm.NewYamlVerifier(tst.rec.Content())
	gm.Verify(tst.t, gm.NewSubstVerifier(verifier, []gm.Replacement{
		{
			Old: tst.tmpDir,
			New: "__top__",
		},
	}))
}

func (tst *virtletManagerTester) listImages(filter *kubeapi.ImageFilter) {
	tst.invoke("ListImages", &kubeapi.ListImagesRequest{Filter: filter})
}

func (tst *virtletManagerTester) pullImage(image *kubeapi.ImageSpec) {
	tst.invoke("PullImage", &kubeapi.PullImageRequest{Image: image})
}

func (tst *virtletManagerTester) imageStatus(image *kubeapi.ImageSpec) {
	tst.invoke("ImageStatus", &kubeapi.ImageStatusRequest{Image: image})
}

func (tst *virtletManagerTester) removeImage(image *kubeapi.ImageSpec) {
	tst.invoke("RemoveImage", &kubeapi.RemoveImageRequest{Image: image})
}

func (tst *virtletManagerTester) listPodSandbox(filter *kubeapi.PodSandboxFilter) {
	tst.invoke("ListPodSandbox", &kubeapi.ListPodSandboxRequest{Filter: filter})
}

func (tst *virtletManagerTester) runPodSandbox(config *kubeapi.PodSandboxConfig) {
	tst.invoke("RunPodSandbox", &kubeapi.RunPodSandboxRequest{Config: config})
}

func (tst *virtletManagerTester) podSandboxStatus(podSandboxID string) {
	tst.invoke("PodSandboxStatus", &kubeapi.PodSandboxStatusRequest{PodSandboxId: podSandboxID})
}

func (tst *virtletManagerTester) stopPodSandox(podSandboxID string) {
	tst.invoke("StopPodSandbox", &kubeapi.StopPodSandboxRequest{PodSandboxId: podSandboxID})
}

func (tst *virtletManagerTester) removePodSandox(podSandboxID string) {
	tst.invoke("RemovePodSandbox", &kubeapi.RemovePodSandboxRequest{PodSandboxId: podSandboxID})
}

func (tst *virtletManagerTester) listContainers(filter *kubeapi.ContainerFilter) {
	tst.invoke("ListContainers", &kubeapi.ListContainersRequest{Filter: filter})
}

func (tst *virtletManagerTester) createContainer(sandbox *kubeapi.PodSandboxConfig, container *criapi.ContainerTestConfig, imageSpec *kubeapi.ImageSpec, mounts []*kubeapi.Mount) string {
	req := createContainerRequest(sandbox, container, imageSpec, mounts)
	resp := tst.invoke("CreateContainer", req)
	if r, ok := resp.(*kubeapi.CreateContainerResponse); ok {
		return r.ContainerId
	} else {
		tst.t.Fatalf("bad value returned by CreateContainer: %#v", resp)
		return "" // unreachable
	}
}

func (tst *virtletManagerTester) containerStatus(containerID string) {
	tst.invoke("ContainerStatus", &kubeapi.ContainerStatusRequest{ContainerId: containerID})
}

func (tst *virtletManagerTester) startContainer(containerID string) {
	tst.invoke("StartContainer", &kubeapi.StartContainerRequest{ContainerId: containerID})
}

func (tst *virtletManagerTester) stopContainer(containerID string) {
	tst.invoke("StopContainer", &kubeapi.StopContainerRequest{ContainerId: containerID})
}

func (tst *virtletManagerTester) removeContainer(containerID string) {
	tst.invoke("RemoveContainer", &kubeapi.RemoveContainerRequest{ContainerId: containerID})
}

func cirrosImg() *kubeapi.ImageSpec {
	// return new object each time b/c in theory it can be
	// modified by the handler
	return &kubeapi.ImageSpec{Image: "localhost/cirros.img"}
}

func ubuntuImg() *kubeapi.ImageSpec {
	// return new object each time b/c in theory it can be
	// modified by the handler
	return &kubeapi.ImageSpec{Image: "localhost/ubuntu.img"}
}

func TestManagerImages(t *testing.T) {
	tst := makeVirtletManagerTester(t)
	defer tst.teardown()
	tst.listImages(nil)
	tst.pullImage(cirrosImg())
	tst.pullImage(ubuntuImg())
	tst.listImages(nil)
	tst.listImages(&kubeapi.ImageFilter{Image: cirrosImg()})
	tst.imageStatus(cirrosImg())
	tst.removeImage(cirrosImg())
	tst.imageStatus(cirrosImg())
	tst.listImages(nil)
	// second RemoveImage() should not cause an error
	tst.removeImage(cirrosImg())
	tst.verify()
}

func createContainerRequest(sandbox *kubeapi.PodSandboxConfig, container *criapi.ContainerTestConfig, imageSpec *kubeapi.ImageSpec, mounts []*kubeapi.Mount) *kubeapi.CreateContainerRequest {
	return &kubeapi.CreateContainerRequest{
		PodSandboxId: sandbox.Metadata.Uid,
		Config: &kubeapi.ContainerConfig{
			Image:  imageSpec,
			Labels: container.Labels,
			Mounts: mounts,
			Metadata: &kubeapi.ContainerMetadata{
				Name: container.Name,
			},
		},
		SandboxConfig: sandbox,
	}
}

func TestManagerPods(t *testing.T) {
	tst := makeVirtletManagerTester(t)
	defer tst.teardown()
	tst.listPodSandbox(nil)
	tst.listContainers(nil)

	sandboxes := criapi.GetSandboxes(2)
	containers := criapi.GetContainersConfig(sandboxes)
	tst.pullImage(cirrosImg())
	tst.runPodSandbox(sandboxes[0])
	tst.listPodSandbox(nil)
	tst.podSandboxStatus(sandboxes[0].Metadata.Uid)
	containerId1 := tst.createContainer(sandboxes[0], containers[0], cirrosImg(), nil)
	tst.listContainers(nil)
	tst.containerStatus(containerId1)
	tst.startContainer(containerId1)
	tst.containerStatus(containerId1)

	tst.pullImage(ubuntuImg())
	tst.runPodSandbox(sandboxes[1])
	containerId2 := tst.createContainer(sandboxes[1], containers[1], ubuntuImg(), nil)
	tst.listPodSandbox(nil)
	tst.listContainers(nil)
	tst.containerStatus(containerId2)
	tst.startContainer(containerId2)
	tst.containerStatus(containerId2)

	tst.stopContainer(containerId1)
	tst.stopContainer(containerId2)
	// this should not cause an error
	tst.stopContainer(containerId2)

	tst.listContainers(nil)
	tst.containerStatus(containerId1)

	tst.removeContainer(containerId1)
	tst.removeContainer(containerId2)
	// this should not cause an error
	tst.removeContainer(containerId2)

	tst.stopPodSandox(sandboxes[0].Metadata.Uid)
	tst.stopPodSandox(sandboxes[1].Metadata.Uid)
	// this should not cause an error
	tst.stopPodSandox(sandboxes[1].Metadata.Uid)

	tst.listPodSandbox(nil)
	tst.podSandboxStatus(sandboxes[0].Metadata.Uid)

	tst.removePodSandox(sandboxes[0].Metadata.Uid)
	tst.removePodSandox(sandboxes[1].Metadata.Uid)
	// this should not cause an error
	tst.removePodSandox(sandboxes[1].Metadata.Uid)

	tst.listPodSandbox(nil)
	tst.listContainers(nil)

	tst.verify()
}

func TestManagerMounts(t *testing.T) {
	tst := makeVirtletManagerTester(t)
	defer tst.teardown()

	sandboxes := criapi.GetSandboxes(1)
	containers := criapi.GetContainersConfig(sandboxes)

	tst.pullImage(cirrosImg())
	tst.runPodSandbox(sandboxes[0])
	tst.podSandboxStatus(sandboxes[0].Metadata.Uid)

	mounts := tst.getSampleFlexvolMounts(sandboxes[0].Metadata.Uid)
	containerId1 := tst.createContainer(sandboxes[0], containers[0], cirrosImg(), mounts)
	tst.containerStatus(containerId1)
	tst.startContainer(containerId1)
	tst.stopContainer(containerId1)
	tst.removeContainer(containerId1)
	tst.stopPodSandox(sandboxes[0].Metadata.Uid)
	tst.removePodSandox(sandboxes[0].Metadata.Uid)
	tst.verify()
}

func TestManagerPodFilters(t *testing.T) {
	tst := makeVirtletManagerTester(t)
	tst.rec.AddFilter("ListPodSandbox")
	defer tst.teardown()

	sandboxes := criapi.GetSandboxes(2)
	sandboxes[1].Labels["foo"] = "bar2"
	tst.runPodSandbox(sandboxes[0])
	tst.runPodSandbox(sandboxes[1])

	tst.listPodSandbox(nil)
	tst.listPodSandbox(&kubeapi.PodSandboxFilter{Id: sandboxes[0].Metadata.Uid})
	tst.listPodSandbox(&kubeapi.PodSandboxFilter{
		State: &kubeapi.PodSandboxStateValue{
			State: kubeapi.PodSandboxState_SANDBOX_READY,
		},
	})
	tst.listPodSandbox(&kubeapi.PodSandboxFilter{
		State: &kubeapi.PodSandboxStateValue{
			State: kubeapi.PodSandboxState_SANDBOX_NOTREADY,
		},
	})
	tst.listPodSandbox(&kubeapi.PodSandboxFilter{
		LabelSelector: map[string]string{
			"foo": "bar2",
		},
	})

	tst.stopPodSandox(sandboxes[1].Metadata.Uid)
	tst.listPodSandbox(&kubeapi.PodSandboxFilter{
		State: &kubeapi.PodSandboxStateValue{
			State: kubeapi.PodSandboxState_SANDBOX_READY,
		},
	})
	tst.listPodSandbox(&kubeapi.PodSandboxFilter{
		State: &kubeapi.PodSandboxStateValue{
			State: kubeapi.PodSandboxState_SANDBOX_NOTREADY,
		},
	})

	tst.verify()
}

func TestManagerContainerFilters(t *testing.T) {
	tst := makeVirtletManagerTester(t)
	tst.rec.AddFilter("ListContainers")
	defer tst.teardown()

	sandboxes := criapi.GetSandboxes(2)
	containers := criapi.GetContainersConfig(sandboxes)
	tst.pullImage(cirrosImg())
	tst.runPodSandbox(sandboxes[0])
	containerId1 := tst.createContainer(sandboxes[0], containers[0], cirrosImg(), nil)
	tst.startContainer(containerId1)
	tst.pullImage(ubuntuImg())
	tst.runPodSandbox(sandboxes[1])
	containerId2 := tst.createContainer(sandboxes[1], containers[1], ubuntuImg(), nil)
	tst.startContainer(containerId2)

	tst.listContainers(nil)
	tst.listContainers(&kubeapi.ContainerFilter{Id: containerId1})
	tst.listContainers(&kubeapi.ContainerFilter{
		State: &kubeapi.ContainerStateValue{
			State: kubeapi.ContainerState_CONTAINER_RUNNING,
		},
	})
	tst.listContainers(&kubeapi.ContainerFilter{
		State: &kubeapi.ContainerStateValue{
			State: kubeapi.ContainerState_CONTAINER_EXITED,
		},
	})
	tst.listContainers(&kubeapi.ContainerFilter{
		LabelSelector: map[string]string{
			"io.kubernetes.pod.name": "testName_1",
		},
	})
	tst.listContainers(&kubeapi.ContainerFilter{
		PodSandboxId: sandboxes[0].Metadata.Uid,
	})

	tst.stopContainer(containerId1)
	tst.listContainers(&kubeapi.ContainerFilter{
		State: &kubeapi.ContainerStateValue{
			State: kubeapi.ContainerState_CONTAINER_RUNNING,
		},
	})
	tst.listContainers(&kubeapi.ContainerFilter{
		State: &kubeapi.ContainerStateValue{
			State: kubeapi.ContainerState_CONTAINER_EXITED,
		},
	})

	tst.verify()
}

// TODO: test Attach / PortForward
// TODO: split grpc-related bits (register, serve) and ImageManager from VirtletManager.
//       Also, remove RecoverAndGC() from it and do image gc via a hook in RemoveContainer()
// TODO: use interceptor for logging in the manager
//       (apply it only if glog level is high enough)
