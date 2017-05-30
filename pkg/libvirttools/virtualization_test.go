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
	"io/ioutil"
	"os"
	"testing"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/bolttools"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/criapi"
)

const (
	fakeImageName = "fake/image1"
	fakeCNIConfig = `{"noCniForNow":true}`
)

func TestContainerLifecycle(t *testing.T) {
	sandboxes := criapi.GetSandboxes(1)
	tmpDir, err := ioutil.TempDir("", "virtualization-test-")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	downloader := utils.NewFakeDownloader(tmpDir)
	domainConn := fake.NewFakeDomainConnection()
	storageConn := fake.NewFakeStorageConnection()

	boltClient, err := bolttools.NewFakeBoltClient()
	if err != nil {
		t.Fatalf("Failed to create fake bolt client: %v", err)
	}
	// TODO: uncomment this after moving image metadata handling to ImageTool
	// if err := boltClient.EnsureImageSchema(); err != nil {
	// 	t.Fatalf("boltClient: failed to create image schema: %v", err)
	// }
	if err := boltClient.EnsureSandboxSchema(); err != nil {
		t.Fatalf("boltClient: failed to create sandbox schema: %v", err)
	}
	if err := boltClient.EnsureVirtualizationSchema(); err != nil {
		t.Fatalf("boltClient: failed to create virtualization schema: %v", err)
	}

	imageTool, err := NewImageTool(storageConn, downloader, "default")

	if err != nil {
		t.Fatalf("Failed to create ImageTool: %v", err)
	}
	virtTool, err := NewVirtualizationTool(domainConn, storageConn, imageTool, boltClient, "volumes", "loop*")
	if err != nil {
		t.Fatalf("failed to create VirtualizationTool: %v", err)
	}

	// TODO: move image metadata store & name conversion to ImageTool
	// (i.e. methods like RemoveImage should accept image name)
	imageVolumeName, err := ImageNameToVolumeName(fakeImageName)
	if err != nil {
		t.Fatalf("Error getting volume name for image %q: %v", fakeImageName, err)
	}

	if _, err := imageTool.PullRemoteImageToVolume(fakeImageName, imageVolumeName); err != nil {
		t.Fatalf("Error pulling image %q to volume %q: %v", fakeImageName, imageVolumeName, err)
	}

	if err := boltClient.SetPodSandbox(sandboxes[0], []byte(fakeCNIConfig)); err != nil {
		t.Fatalf("Failed to store pod sandbox: %v", err)
	}

	containers, err := virtTool.ListContainers(nil)
	switch {
	case err != nil:
		t.Fatalf("ListContainers() failed: %v", err)
	case len(containers) != 0:
		t.Errorf("Unexpected containers when no containers are started: %#v", containers)
	}

	containerId, err := virtTool.CreateContainer(&kubeapi.CreateContainerRequest{
		PodSandboxId: sandboxes[0].Metadata.Uid,
		Config: &kubeapi.ContainerConfig{
			Metadata: &kubeapi.ContainerMetadata{
				Name: "container1",
			},
			Image: &kubeapi.ImageSpec{
				Image: fakeImageName,
			},
		},
		SandboxConfig: sandboxes[0],
	}, "/tmp/fakenetns", fakeCNIConfig)
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}

	containers, err = virtTool.ListContainers(nil)
	switch {
	case err != nil:
		t.Errorf("ListContainers() failed: %v", err)
	case len(containers) != 1:
		t.Errorf("Expected single container to be started, but got: %#v", containers)
	case containers[0].Id != containerId:
		t.Errorf("Bad container id in response: %q instead of %q", containers[0].Id, containerId)
	case containers[0].State != kubeapi.ContainerState_CONTAINER_CREATED:
		t.Errorf("Bad container state: %v instead of %v", containers[0].State, kubeapi.ContainerState_CONTAINER_CREATED)
	}

	if err = virtTool.StartContainer(containerId); err != nil {
		t.Fatalf("StartContainer failed for container %q: %v", containerId, err)
	}

	status, err := virtTool.ContainerStatus(containerId)
	switch {
	case err != nil:
		t.Errorf("ContainerStatus(): %v", err)
	case status.State != kubeapi.ContainerState_CONTAINER_RUNNING:
		t.Errorf("Bad container state: %v instead of %v", containers[0].State, kubeapi.ContainerState_CONTAINER_RUNNING)
	}

	if err = virtTool.StopContainer(containerId); err != nil {
		t.Fatalf("StopContainer failed for container %q: %v", containerId, err)
	}

	status, err = virtTool.ContainerStatus(containerId)
	switch {
	case err != nil:
		t.Errorf("ContainerStatus(): %v", err)
	case status.State != kubeapi.ContainerState_CONTAINER_EXITED:
		t.Errorf("Bad container state: %v instead of %v", containers[0].State, kubeapi.ContainerState_CONTAINER_EXITED)
	}

	if err = virtTool.RemoveContainer(containerId); err != nil {
		t.Fatalf("RemoveContainer failed for container %q: %v", containerId, err)
	}

	containers, err = virtTool.ListContainers(nil)
	switch {
	case err != nil:
		t.Fatalf("ListContainers() failed: %v", err)
	case len(containers) != 0:
		t.Errorf("Unexpected containers when no containers are started: %#v", containers)
	}
}
