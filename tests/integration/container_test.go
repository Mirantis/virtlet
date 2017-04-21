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

package integration

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/tests/criapi"
)

type containerTester struct {
	t                    *testing.T
	manager              *VirtletManager
	runtimeServiceClient kubeapi.RuntimeServiceClient
	imageServiceClient   kubeapi.ImageServiceClient
	sandboxes            []*kubeapi.PodSandboxConfig
	containers           []*criapi.ContainerTestConfig
	imageSpecs           []*kubeapi.ImageSpec
}

func newContainerTester(t *testing.T) *containerTester {
	manager := NewVirtletManager()
	if err := manager.Run(); err != nil {
		t.Fatal(err)
	}

	sandboxes := criapi.GetSandboxes(2)
	return &containerTester{
		t:                    t,
		manager:              manager,
		runtimeServiceClient: kubeapi.NewRuntimeServiceClient(manager.conn),
		imageServiceClient:   kubeapi.NewImageServiceClient(manager.conn),
		sandboxes:            sandboxes,
		containers:           criapi.GetContainersConfig(sandboxes),
		imageSpecs: []*kubeapi.ImageSpec{
			{Image: imageCirrosUrl},
			{Image: imageCirrosUrl2},
		},
	}
}

func (ct *containerTester) teardown() {
	ct.manager.Close()
}

func (ct *containerTester) pullImage(imageSpec *kubeapi.ImageSpec) {
	in := &kubeapi.PullImageRequest{
		Image:         imageSpec,
		Auth:          &kubeapi.AuthConfig{},
		SandboxConfig: &kubeapi.PodSandboxConfig{},
	}
	if _, err := ct.imageServiceClient.PullImage(context.Background(), in); err != nil {
		ct.t.Fatal(err)
	}
}

func (ct *containerTester) pullAllImages() {
	for _, imageSpec := range ct.imageSpecs {
		ct.pullImage(imageSpec)
	}
}

func (ct *containerTester) runPodSandbox(sandbox *kubeapi.PodSandboxConfig) *kubeapi.RunPodSandboxResponse {
	resp, err := ct.runtimeServiceClient.RunPodSandbox(context.Background(), &kubeapi.RunPodSandboxRequest{Config: sandbox})
	if err != nil {
		ct.t.Fatal(err)
	}
	sandbox.Metadata.Uid = resp.PodSandboxId
	return resp
}

func (ct *containerTester) createContainer(sandbox *kubeapi.PodSandboxConfig, container *criapi.ContainerTestConfig, imageSpec *kubeapi.ImageSpec, mounts []*kubeapi.Mount) *kubeapi.CreateContainerResponse {
	// Container request
	config := &kubeapi.ContainerConfig{
		Image:  imageSpec,
		Labels: container.Labels,
		Mounts: mounts,
		Metadata: &kubeapi.ContainerMetadata{
			Name: container.Name,
		},
	}
	containerIn := &kubeapi.CreateContainerRequest{
		PodSandboxId:  sandbox.Metadata.Uid,
		Config:        config,
		SandboxConfig: sandbox,
	}

	resp, err := ct.runtimeServiceClient.CreateContainer(context.Background(), containerIn)
	if err != nil {
		ct.t.Fatalf("Creating container %s failure: %v", sandbox.Metadata.Name, err)
	}
	container.ContainerId = resp.ContainerId
	ct.t.Logf("Container created: %q", container.ContainerId)
	return resp
}

func (ct *containerTester) startContainer(containerId string) {
	_, err := ct.runtimeServiceClient.StartContainer(context.Background(), &kubeapi.StartContainerRequest{
		ContainerId: containerId,
	})
	if err != nil {
		ct.t.Fatalf("Error starting container %s: %v", containerId, err)
	}
}

func (ct *containerTester) listContainers(filter *kubeapi.ContainerFilter) *kubeapi.ListContainersResponse {
	resp, err := ct.runtimeServiceClient.ListContainers(context.Background(), &kubeapi.ListContainersRequest{
		Filter: filter,
	})
	if err != nil {
		ct.t.Fatalf("Listing containers failure: %v", err)
	}
	return resp
}

func (ct *containerTester) waitForContainerRunning(containerId, containerName string) {
	containerStatusRequest := &kubeapi.ContainerStatusRequest{
		ContainerId: containerId,
	}
	err := utils.WaitLoop(func() (bool, error) {
		resp, err := ct.runtimeServiceClient.ContainerStatus(context.Background(), containerStatusRequest)
		if err != nil {
			return false, err
		}

		if containerName != "" && resp.Status.Metadata.Name != containerName {
			return false, fmt.Errorf("bad container name returned: %q instead of %q",
				containerName, resp.Status.Metadata.Name)
		}

		if resp.GetStatus().State == kubeapi.ContainerState_CONTAINER_RUNNING {
			return true, nil
		}

		return false, nil
	}, time.Second, 20*time.Second)

	if err != nil {
		ct.t.Errorf("Container not reaching RUNNING state: %v", err)
	}
}

func (ct *containerTester) stopContainer(containerId string) {
	_, err := ct.runtimeServiceClient.StopContainer(context.Background(), &kubeapi.StopContainerRequest{
		ContainerId: containerId,
	})
	if err != nil {
		ct.t.Fatalf("Error stopping container %s: %v", containerId, err)
	}
}

func (ct *containerTester) removeContainer(containerId string) {
	_, err := ct.runtimeServiceClient.RemoveContainer(context.Background(), &kubeapi.RemoveContainerRequest{
		ContainerId: containerId,
	})
	if err != nil {
		ct.t.Fatalf("Error removing container %s: %v", containerId, err)
	}
}

func (ct *containerTester) waitForNoContainers(filter *kubeapi.ContainerFilter) {
	// Wait up to 20 seconds for container removal
	err := utils.WaitLoop(func() (bool, error) {
		out, err := ct.runtimeServiceClient.ListContainers(context.Background(), &kubeapi.ListContainersRequest{
			Filter: filter,
		})
		if err != nil {
			return false, err
		}

		return len(out.Containers) == 0, nil
	}, time.Second, 20*time.Second)
	if err != nil {
		ct.t.Fatalf("Container not removed in expected time: %v", err)
	}
}
