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
	"testing"
	"time"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/tests/criapi"

	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func TestContainerStatuses(t *testing.T) {
	if inTravis() {
		// Env vars are not passed to /vmwrapper
		// QEMU fails with:
		// Failed to unlink socket /var/lib/libvirt/qemu/capabilities.monitor.sock: Permission denied
		// Running libvirt in non-build container works though
		t.Skip("TestContainerStatuses fails in Travis due to a libvirt+qemu problem in build container")
	}
	manager := NewVirtletManager()
	if err := manager.Run(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	runtimeServiceClient := kubeapi.NewRuntimeServiceClient(manager.conn)

	sandboxes, err := criapi.GetSandboxes(1)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}
	sandbox := sandboxes[0]

	containers, err := criapi.GetContainersConfig(sandboxes)
	if err != nil {
		t.Fatalf("Failed to generate array of container configs: %v", err)
	}
	container := containers[0]

	// Pull Images
	imageServiceClient := kubeapi.NewImageServiceClient(manager.conn)

	imageSpec := &kubeapi.ImageSpec{
		Image: &imageCirrosUrl,
	}

	in := &kubeapi.PullImageRequest{
		Image:         imageSpec,
		Auth:          &kubeapi.AuthConfig{},
		SandboxConfig: &kubeapi.PodSandboxConfig{},
	}

	if _, err := imageServiceClient.PullImage(context.Background(), in); err != nil {
		t.Fatal(err)
	}

	sandboxOut, err := runtimeServiceClient.RunPodSandbox(context.Background(), &kubeapi.RunPodSandboxRequest{Config: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	sandbox.Metadata.Uid = sandboxOut.PodSandboxId

	// Container request
	config := &kubeapi.ContainerConfig{
		Image:  imageSpec,
		Labels: container.Labels,
		Metadata: &kubeapi.ContainerMetadata{
			Name: sandbox.Metadata.Name,
		},
	}
	containerIn := &kubeapi.CreateContainerRequest{
		PodSandboxId:  sandboxOut.PodSandboxId,
		Config:        config,
		SandboxConfig: sandbox,
	}

	createContainerOut, err := runtimeServiceClient.CreateContainer(context.Background(), containerIn)
	if err != nil {
		t.Fatalf("Creating container %s failure: %v", *sandbox.Metadata.Name, err)
	}
	t.Logf("Container created Sandbox: %v\n", sandbox)
	container.ContainerId = *createContainerOut.ContainerId

	_, err = runtimeServiceClient.StartContainer(context.Background(), &kubeapi.StartContainerRequest{ContainerId: &container.ContainerId})
	if err != nil {
		t.Fatalf("Starting container %s failure: %v", container.ContainerId, err)
	}

	listContainersRequest := &kubeapi.ListContainersRequest{
		Filter: &kubeapi.ContainerFilter{
			Id: &container.ContainerId,
		},
	}

	listContainersOut, err := runtimeServiceClient.ListContainers(context.Background(), listContainersRequest)
	if err != nil {
		t.Fatalf("Listing containers failure: %v", err)
	}

	if len(listContainersOut.Containers) != 1 {
		t.Errorf("Expected single container, instead got: %d", len(listContainersOut.Containers))
	}

	if *listContainersOut.Containers[0].Id != container.ContainerId {
		t.Errorf("Didn't find expected container id %s in returned containers list %v", container.ContainerId, listContainersOut.Containers)
	}

	// Wait up to 20 seconds for container ready status
	containerStatusRequest := &kubeapi.ContainerStatusRequest{
		ContainerId: &container.ContainerId,
	}
	err = utils.WaitLoop(func() (bool, error) {
		status, err := runtimeServiceClient.ContainerStatus(context.Background(), containerStatusRequest)
		if err != nil {
			return false, err
		}

		if status.GetStatus().GetState() == kubeapi.ContainerState_CONTAINER_RUNNING {
			return true, nil
		}

		return false, nil
	}, time.Second, 20*time.Second)

	if err != nil {
		t.Errorf("Container not reaching RUNNING state: %v", err)
	}

	// Stop container request
	containerStopIn := &kubeapi.StopContainerRequest{
		ContainerId: &containers[0].ContainerId,
	}
	_, err = runtimeServiceClient.StopContainer(context.Background(), containerStopIn)
	if err != nil {
		t.Fatalf("Stopping container %s failure: %v", container.ContainerId, err)
	}

	// Remove container request
	containerRemoveIn := &kubeapi.RemoveContainerRequest{
		ContainerId: &container.ContainerId,
	}
	_, err = runtimeServiceClient.RemoveContainer(context.Background(), containerRemoveIn)
	if err != nil {
		t.Fatalf("Removing container %s failure: %v", container.ContainerId, err)
	}

	// Wait up to 20 seconds for container removal
	err = utils.WaitLoop(func() (bool, error) {
		out, err := runtimeServiceClient.ListContainers(context.Background(), listContainersRequest)
		if err != nil {
			return false, err
		}

		return len(out.Containers) == 0, nil
	}, time.Second, 20*time.Second)
	if err != nil {
		t.Fatalf("Container not removed in expected time: %v", err)
	}
}
