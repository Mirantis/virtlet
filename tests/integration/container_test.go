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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/flexvolume"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/tests/criapi"
)

const (
	// TODO: this is only ok inside the build container.
	// Should use a temporary directory for fake pod dir
	kubeletRootDir = "/var/lib/kubelet/pods"
)

type containerTester struct {
	t                    *testing.T
	manager              *VirtletManager
	runtimeServiceClient kubeapi.RuntimeServiceClient
	imageServiceClient   kubeapi.ImageServiceClient
	sandboxes            []*kubeapi.PodSandboxConfig
	containers           []*criapi.ContainerTestConfig
	imageSpecs           []*kubeapi.ImageSpec
	fv                   *flexvolume.FlexVolumeDriver
	podDirs              []string
	volumeDirs           []string
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
		fv: flexvolume.NewFlexVolumeDriver(utils.NewUuid, flexvolume.NewLinuxMounter()),
	}
}

func (ct *containerTester) cleanupContainers() {
	ctx := context.Background()
	resp, err := ct.runtimeServiceClient.ListContainers(ctx, &kubeapi.ListContainersRequest{})
	if err != nil {
		ct.t.Log("warning: couldn't list containers")
	}
	for _, container := range resp.Containers {
		_, err := ct.runtimeServiceClient.StopContainer(ctx, &kubeapi.StopContainerRequest{
			ContainerId: container.Id,
		})
		if err != nil {
			ct.t.Log("warning: couldn't stop container %q", container.Id)
		}
		_, err = ct.runtimeServiceClient.RemoveContainer(ctx, &kubeapi.RemoveContainerRequest{
			ContainerId: container.Id,
		})
		if err != nil {
			ct.t.Log("warning: couldn't remove container %q", container.Id)
		}
	}
}

func (ct *containerTester) cleanupPods() {
	ctx := context.Background()
	podList, err := ct.runtimeServiceClient.ListPodSandbox(ctx, &kubeapi.ListPodSandboxRequest{})
	if err != nil {
		ct.t.Log("warning: couldn't list pods for removal")
		return
	}
	for _, pod := range podList.Items {
		_, err := ct.runtimeServiceClient.StopPodSandbox(ctx, &kubeapi.StopPodSandboxRequest{
			PodSandboxId: pod.Id,
		})
		if err != nil {
			ct.t.Log("warning: couldn't stop pod sandbox %q", pod.Id)
		}
		_, err = ct.runtimeServiceClient.RemovePodSandbox(ctx, &kubeapi.RemovePodSandboxRequest{
			PodSandboxId: pod.Id,
		})
		if err != nil {
			ct.t.Log("warning: couldn't remove container %q", pod.Id)
		}
	}
}

func (ct *containerTester) cleanupKubeletRoot() {
	for _, volumeDir := range ct.volumeDirs {
		if err := ct.runFlexvolumeDriver("unmount", volumeDir); err != nil {
			ct.t.Error(err)
		}
	}
	for _, podDir := range ct.podDirs {
		if err := os.RemoveAll(podDir); err != nil {
			ct.t.Errorf("warning: couldn't remove pod dir %q", podDir)
		}
	}
}

func (ct *containerTester) teardown() {
	ct.cleanupContainers()
	ct.cleanupPods()
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

func (ct *containerTester) callCreateContainer(sandbox *kubeapi.PodSandboxConfig, container *criapi.ContainerTestConfig, imageSpec *kubeapi.ImageSpec, mounts []*kubeapi.Mount) (*kubeapi.CreateContainerResponse, error) {
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

	return ct.runtimeServiceClient.CreateContainer(context.Background(), containerIn)
}

func (ct *containerTester) createContainer(sandbox *kubeapi.PodSandboxConfig, container *criapi.ContainerTestConfig, imageSpec *kubeapi.ImageSpec, mounts []*kubeapi.Mount) *kubeapi.CreateContainerResponse {
	resp, err := ct.callCreateContainer(sandbox, container, imageSpec, mounts)
	if err != nil {
		ct.t.Fatalf("Creating container %s failure: %v", sandbox.Metadata.Name, err)
	}
	container.ContainerId = resp.ContainerId
	ct.t.Logf("Container created: %q", container.ContainerId)
	return resp
}

func (ct *containerTester) callStartContainer(containerId string) error {
	_, err := ct.runtimeServiceClient.StartContainer(context.Background(), &kubeapi.StartContainerRequest{
		ContainerId: containerId,
	})
	return err
}

func (ct *containerTester) startContainer(containerId string) {
	if err := ct.callStartContainer(containerId); err != nil {
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

func (ct *containerTester) verifyNoContainers(filter *kubeapi.ContainerFilter) {
	if len(ct.listContainers(filter).Containers) != 0 {
		ct.t.Errorf("expected no containers to be listed, filter: %s", spew.Sdump(filter))
	}
}

func (ct *containerTester) runFlexvolumeDriver(args ...string) error {
	r := ct.fv.Run(args)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(r), &m); err != nil {
		return fmt.Errorf("failed to unmarshal flexvolume result (args %#v): %v", args, err)
	}
	if m["status"] != "Success" {
		return fmt.Errorf("flexvolume driver failed, args %#v, result: %s", args, r)
	}
	return nil
}

func (ct *containerTester) mountFlexvolume(podId, name string, opts map[string]interface{}) {
	podDir := fmt.Sprintf("/var/lib/kubelet/pods/%s", podId)
	volumeDir := filepath.Join(podDir, "volumes/virtlet~flexvolume_driver/"+name)
	if err := os.MkdirAll(volumeDir, 0755); err != nil {
		ct.t.Fatalf("can't create volume dir: %v", err)
	}
	ct.podDirs = append(ct.podDirs, podDir)
	ct.volumeDirs = append(ct.volumeDirs, volumeDir)

	// Here we simulate what kubelet is doing by invoking our flexvolume
	// driver directly.
	// XXX: there's a subtle difference between what we do here and
	// what happens on the real system though. In the latter case
	// virtlet pod doesn't see the contents of tmpfs because hostPath volumes
	// are mounted privately into the virtlet pod mount ns. Here we
	// let Virtlet process tmpfs contents. Currently the contents
	// of flexvolume's tmpfs and the shadowed directory should be the
	// same though.
	if err := ct.runFlexvolumeDriver("mount", volumeDir, utils.MapToJson(opts)); err != nil {
		ct.t.Fatal(err)
	}
}
