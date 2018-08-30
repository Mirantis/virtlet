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

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"

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
	manager := NewVirtletManager(t)
	manager.Run()

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
			{Image: imageCopyCirrosUrl},
		},
		fv: flexvolume.NewFlexVolumeDriver(utils.NewUUID, utils.NewMounter()),
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
			ct.t.Logf("warning: couldn't stop container %q", container.Id)
		}
		_, err = ct.runtimeServiceClient.RemoveContainer(ctx, &kubeapi.RemoveContainerRequest{
			ContainerId: container.Id,
		})
		if err != nil {
			ct.t.Logf("warning: couldn't remove container %q", container.Id)
		}
	}
}

func (ct *containerTester) cleanupPods() {
	for _, pod := range ct.listPodSandbox(nil) {
		ct.stopPodSandbox(pod.Id)
		ct.removePodSandbox(pod.Id)
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

func (ct *containerTester) listPodSandbox(filter *kubeapi.PodSandboxFilter) []*kubeapi.PodSandbox {
	resp, err := ct.runtimeServiceClient.ListPodSandbox(context.Background(), &kubeapi.ListPodSandboxRequest{
		Filter: filter,
	})
	if err != nil {
		ct.t.Errorf("warning: couldn't list pods: %v", err)
		return nil
	}
	return resp.Items
}

func (ct *containerTester) stopPodSandbox(podSandboxId string) {
	_, err := ct.runtimeServiceClient.StopPodSandbox(context.Background(), &kubeapi.StopPodSandboxRequest{
		PodSandboxId: podSandboxId,
	})
	if err != nil {
		ct.t.Errorf("warning: couldn't stop pod sandbox %q: %v", podSandboxId, err)
	}
}

func (ct *containerTester) removePodSandbox(podSandboxId string) {
	_, err := ct.runtimeServiceClient.RemovePodSandbox(context.Background(), &kubeapi.RemovePodSandboxRequest{
		PodSandboxId: podSandboxId,
	})
	if err != nil {
		ct.t.Errorf("warning: couldn't stop pod sandbox %q: %v", podSandboxId, err)
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

func (ct *containerTester) verifyPodSandboxStateViaStatus(podSandboxId, podSandboxName string, expectedState kubeapi.PodSandboxState) {
	resp, err := ct.runtimeServiceClient.PodSandboxStatus(context.Background(), &kubeapi.PodSandboxStatusRequest{
		PodSandboxId: podSandboxId,
	})
	if err != nil {
		ct.t.Errorf("PodSandboxStatus() failed for pod sandbox %q (id %q): %v", podSandboxName, podSandboxId, err)
		return
	}

	if podSandboxName != "" && resp.Status.Metadata.Name != podSandboxName {
		ct.t.Errorf("Bad pod sandbox name returned: %q instead of %q",
			podSandboxName, resp.Status.Metadata.Name)
	}

	if resp.Status == nil {
		ct.t.Errorf("Null pod sandbox status returned for pod sandbox %q (id %q)", podSandboxName, podSandboxId)
		return
	}
	if resp.Status.State != expectedState {
		ct.t.Errorf("Bad pod sandbox state: %v instead of %v", resp.GetStatus().State, expectedState)
	}
}

func (ct *containerTester) verifyPodSandboxStateViaList(podSandboxId, podSandboxName string, expectedState kubeapi.PodSandboxState) {
	pods := ct.listPodSandbox(nil)
	if pods == nil {
		// this means an error that's already reported by listPodSandbox()
		return
	}

	if len(pods) != 1 {
		ct.t.Errorf("Bad pod sandbox list returned by ListPodSandbox() (expected 1 pod sandbox):\n%s", spew.Sdump(pods))
		return
	}

	if podSandboxName != "" && pods[0].Metadata.Name != podSandboxName {
		ct.t.Errorf("Bad pod sandbox name in ListPodSandbox() response: %q instead of %q",
			podSandboxName, pods[0].Metadata.Name)
	}

	if pods[0].State != expectedState {
		ct.t.Errorf("Bad pod sandbox state in ListPodSandbox() response: %v instead of %v", pods[0].State, expectedState)
	}
}

func (ct *containerTester) verifyPodSandboxState(sandbox *kubeapi.PodSandboxConfig, expectedState kubeapi.PodSandboxState) {
	ct.verifyPodSandboxStateViaStatus(sandbox.Metadata.Uid, sandbox.Metadata.Name, expectedState)
	ct.verifyPodSandboxStateViaList(sandbox.Metadata.Uid, sandbox.Metadata.Name, expectedState)
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
	container.ContainerID = resp.ContainerId
	ct.t.Logf("Container created: %q", container.ContainerID)
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

func (ct *containerTester) verifyContainerStateViaStatus(containerId, containerName string, expectedState kubeapi.ContainerState) {
	resp, err := ct.runtimeServiceClient.ContainerStatus(context.Background(), &kubeapi.ContainerStatusRequest{
		ContainerId: containerId,
	})
	if err != nil {
		ct.t.Errorf("ContainerStatus() failed for container %q (id %q): %v", containerName, containerId, err)
		return
	}

	if containerName != "" && resp.Status.Metadata.Name != containerName {
		ct.t.Errorf("Bad container name returned: %q instead of %q",
			containerName, resp.Status.Metadata.Name)
	}

	if resp.Status == nil {
		ct.t.Errorf("Null container status returned for container %q (id %q)", containerName, containerId)
		return
	}
	if resp.Status.State != expectedState {
		ct.t.Errorf("Bad container state: %v instead of %v", resp.GetStatus().State, expectedState)
	}
}

func (ct *containerTester) verifyContainerStateViaList(containerId, containerName string, expectedState kubeapi.ContainerState) {
	resp, err := ct.runtimeServiceClient.ListContainers(context.Background(), &kubeapi.ListContainersRequest{
		Filter: &kubeapi.ContainerFilter{
			Id: containerId,
		},
	})
	if err != nil {
		ct.t.Errorf("ListContainers() failed when called with filter for container %q (id %q): %v", containerName, containerId, err)
		return
	}

	if len(resp.Containers) != 1 {
		ct.t.Errorf("Bad container list returned by ListContainers() (expected 1 container):\n%s", spew.Sdump(resp.Containers))
		return
	}

	if containerName != "" && resp.Containers[0].Metadata.Name != containerName {
		ct.t.Errorf("Bad container name in ListContainers() response: %q instead of %q",
			containerName, resp.Containers[0].Metadata.Name)
	}

	if resp.Containers[0].State != expectedState {
		ct.t.Errorf("Bad container state in ListContainers() response: %v instead of %v", resp.Containers[0].State, expectedState)
	}
}

func (ct *containerTester) verifyContainerState(containerId, containerName string, expectedState kubeapi.ContainerState) {
	ct.verifyContainerStateViaStatus(containerId, containerName, expectedState)
	ct.verifyContainerStateViaList(containerId, containerName, expectedState)
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
	if err := ct.runFlexvolumeDriver("mount", volumeDir, utils.MapToJSON(opts)); err != nil {
		ct.t.Fatal(err)
	}
}
