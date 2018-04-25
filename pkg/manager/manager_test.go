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
	"github.com/Mirantis/virtlet/pkg/image"
	fakeimage "github.com/Mirantis/virtlet/pkg/image/fake"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
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
	t       *testing.T
	rec     *testutils.TopLevelRecorder
	manager *VirtletManager
	tmpDir  string
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
	virtTool.SetKubeletRootDir(filepath.Join(tmpDir, "kubelet-root"))
	manager := NewVirtletManager(virtTool, imageStore, metadataStore, fdManager, translateImageName)
	manager.clock = clock
	return &virtletManagerTester{
		t:       t,
		rec:     rec,
		manager: manager,
		tmpDir:  tmpDir,
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

func (tst *virtletManagerTester) verify() {
	gm.Verify(tst.t, gm.NewYamlVerifier(tst.rec.Content()))
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
	tst.invoke("ListImages", &kubeapi.ListImagesRequest{})
	tst.invoke("PullImage", &kubeapi.PullImageRequest{Image: cirrosImg()})
	tst.invoke("PullImage", &kubeapi.PullImageRequest{Image: ubuntuImg()})
	tst.invoke("ListImages", &kubeapi.ListImagesRequest{})
	tst.invoke("ListImages", &kubeapi.ListImagesRequest{
		Filter: &kubeapi.ImageFilter{Image: cirrosImg()},
	})
	tst.invoke("ImageStatus", &kubeapi.ImageStatusRequest{Image: cirrosImg()})
	tst.invoke("RemoveImage", &kubeapi.RemoveImageRequest{Image: cirrosImg()})
	tst.invoke("ImageStatus", &kubeapi.ImageStatusRequest{Image: cirrosImg()})
	tst.invoke("ListImages", &kubeapi.ListImagesRequest{})
	// second RemoveImage() should not cause an error
	tst.invoke("RemoveImage", &kubeapi.RemoveImageRequest{Image: cirrosImg()})
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
	tst.invoke("ListPodSandbox", &kubeapi.ListPodSandboxRequest{})
	tst.invoke("ListContainers", &kubeapi.ListContainersRequest{})

	sandboxes := criapi.GetSandboxes(2)
	containers := criapi.GetContainersConfig(sandboxes)
	tst.invoke("PullImage", &kubeapi.PullImageRequest{Image: cirrosImg()})
	tst.invoke("RunPodSandbox", &kubeapi.RunPodSandboxRequest{Config: sandboxes[0]})
	tst.invoke("ListPodSandbox", &kubeapi.ListPodSandboxRequest{})
	tst.invoke("PodSandboxStatus", &kubeapi.PodSandboxStatusRequest{PodSandboxId: sandboxes[0].Metadata.Uid})
	containerId1 := tst.invoke(
		"CreateContainer",
		createContainerRequest(sandboxes[0], containers[0], cirrosImg(), nil),
	).(*kubeapi.CreateContainerResponse).ContainerId
	tst.invoke("ListContainers", &kubeapi.ListContainersRequest{})
	tst.invoke("ContainerStatus", &kubeapi.ContainerStatusRequest{ContainerId: containerId1})
	tst.invoke("StartContainer", &kubeapi.StartContainerRequest{ContainerId: containerId1})
	tst.invoke("ContainerStatus", &kubeapi.ContainerStatusRequest{ContainerId: containerId1})

	tst.invoke("PullImage", &kubeapi.PullImageRequest{Image: ubuntuImg()})
	tst.invoke("RunPodSandbox", &kubeapi.RunPodSandboxRequest{Config: sandboxes[1]})
	containerId2 := tst.invoke(
		"CreateContainer",
		createContainerRequest(sandboxes[1], containers[1], ubuntuImg(), nil),
	).(*kubeapi.CreateContainerResponse).ContainerId
	tst.invoke("ListPodSandbox", &kubeapi.ListPodSandboxRequest{})
	tst.invoke("ListContainers", &kubeapi.ListContainersRequest{})
	tst.invoke("ContainerStatus", &kubeapi.ContainerStatusRequest{ContainerId: containerId2})
	tst.invoke("StartContainer", &kubeapi.StartContainerRequest{ContainerId: containerId2})
	tst.invoke("ContainerStatus", &kubeapi.ContainerStatusRequest{ContainerId: containerId2})

	tst.invoke("StopContainer", &kubeapi.StopContainerRequest{ContainerId: containerId1})
	tst.invoke("StopContainer", &kubeapi.StopContainerRequest{ContainerId: containerId2})
	// this should not cause an error
	tst.invoke("StopContainer", &kubeapi.StopContainerRequest{ContainerId: containerId2})

	tst.invoke("ListContainers", &kubeapi.ListContainersRequest{})
	tst.invoke("ContainerStatus", &kubeapi.ContainerStatusRequest{ContainerId: containerId1})

	tst.invoke("RemoveContainer", &kubeapi.RemoveContainerRequest{ContainerId: containerId1})
	tst.invoke("RemoveContainer", &kubeapi.RemoveContainerRequest{ContainerId: containerId2})
	// this should not cause an error
	tst.invoke("RemoveContainer", &kubeapi.RemoveContainerRequest{ContainerId: containerId2})
	tst.invoke("StopPodSandbox", &kubeapi.StopPodSandboxRequest{PodSandboxId: sandboxes[0].Metadata.Uid})
	tst.invoke("StopPodSandbox", &kubeapi.StopPodSandboxRequest{PodSandboxId: sandboxes[1].Metadata.Uid})
	// this should not cause an error
	tst.invoke("StopPodSandbox", &kubeapi.StopPodSandboxRequest{PodSandboxId: sandboxes[1].Metadata.Uid})

	tst.invoke("ListPodSandbox", &kubeapi.ListPodSandboxRequest{})
	tst.invoke("PodSandboxStatus", &kubeapi.PodSandboxStatusRequest{PodSandboxId: sandboxes[0].Metadata.Uid})

	tst.invoke("RemovePodSandbox", &kubeapi.RemovePodSandboxRequest{PodSandboxId: sandboxes[0].Metadata.Uid})
	tst.invoke("RemovePodSandbox", &kubeapi.RemovePodSandboxRequest{PodSandboxId: sandboxes[1].Metadata.Uid})
	// this should not cause an error
	tst.invoke("RemovePodSandbox", &kubeapi.RemovePodSandboxRequest{PodSandboxId: sandboxes[1].Metadata.Uid})

	tst.invoke("ListPodSandbox", &kubeapi.ListPodSandboxRequest{})
	tst.invoke("ListContainers", &kubeapi.ListContainersRequest{})

	tst.verify()
}

// TODO: test mounts
// TODO: test filtering pods/containers by their id
// TODO: test filtering pods/containers by status
// TODO: test filtering pods/containers by labels
// TODO: test filtering containers by pod id
// TODO: test Attach / PortForward
// TODO: split grpc-related bits (register, serve) and ImageManager from VirtletManager.
//       Also, remove RecoverAndGC() from it and do image gc via a hook in RemoveContainer()
// TODO: use interceptor for logging in the manager
//       (apply it only if glog level is high enough)
