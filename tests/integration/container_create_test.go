/*
Copyright 2016 Mirantis

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

	"github.com/Mirantis/virtlet/pkg/bolttools"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func TestContainerCreate(t *testing.T) {
	manager := NewVirtletManager()
	if err := manager.Run(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	runtimeServiceClient := kubeapi.NewRuntimeServiceClient(manager.conn)

	sandboxes, err := bolttools.GetSandboxes(2)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}

	containers, err := bolttools.GetContainersConfig(sandboxes)
	if err != nil {
		t.Fatalf("Failed to generate array of container configs: %v", err)
	}

	containers[0].Labels = map[string]string{"unique": "first", "common": "both"}
	containers[1].Labels = map[string]string{"unique": "second", "common": "both"}

	filterTests := []struct {
		containerFilter *kubeapi.ContainerFilter
		expectedIds     []*string
	}{
		{
			containerFilter: &kubeapi.ContainerFilter{
				Id: &containers[0].ContainerId,
			},
			expectedIds: []*string{&containers[0].ContainerId},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				PodSandboxId: &containers[0].SandboxId,
			},
			expectedIds: []*string{&containers[0].ContainerId},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				PodSandboxId:  &containers[0].SandboxId,
				LabelSelector: map[string]string{"unique": "first", "common": "both"},
			},
			expectedIds: []*string{&containers[0].ContainerId},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				PodSandboxId:  &containers[0].SandboxId,
				LabelSelector: map[string]string{"unique": "nomatch"},
			},
			expectedIds: []*string{},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				Id:           &containers[0].ContainerId,
				PodSandboxId: &containers[0].SandboxId,
			},
			expectedIds: []*string{&containers[0].ContainerId},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				Id:            &containers[0].ContainerId,
				PodSandboxId:  &containers[0].SandboxId,
				LabelSelector: map[string]string{"unique": "first", "common": "both"},
			},
			expectedIds: []*string{&containers[0].ContainerId},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				Id:            &containers[0].ContainerId,
				PodSandboxId:  &containers[0].SandboxId,
				LabelSelector: map[string]string{"unique": "nomatch"},
			},
			expectedIds: []*string{},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				LabelSelector: map[string]string{"unique": "first", "common": "both"},
			},
			expectedIds: []*string{&containers[0].ContainerId},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{
				LabelSelector: map[string]string{"common": "both"},
			},
			expectedIds: []*string{&containers[0].ContainerId, &containers[1].ContainerId},
		},
		{
			containerFilter: &kubeapi.ContainerFilter{},
			expectedIds:     []*string{&containers[0].ContainerId, &containers[1].ContainerId},
		},
	}

	// Pull Images
	imageServiceClient := kubeapi.NewImageServiceClient(manager.conn)

	imageSpecs := []*kubeapi.ImageSpec{
		{Image: &imageCirrosUrl},
		{Image: &imageCirrosUrl2},
	}

	for _, ispec := range imageSpecs {
		in := &kubeapi.PullImageRequest{
			Image:         ispec,
			Auth:          &kubeapi.AuthConfig{},
			SandboxConfig: &kubeapi.PodSandboxConfig{},
		}

		if _, err := imageServiceClient.PullImage(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}

	for ind, sandbox := range sandboxes {
		// Sandbox request
		sandboxOut, err := runtimeServiceClient.RunPodSandbox(context.Background(), &kubeapi.RunPodSandboxRequest{Config: sandbox})
		if err != nil {
			t.Fatal(err)
		}
		sandboxes[ind].Metadata.Uid = sandboxOut.PodSandboxId

		// Container request
		hostPath := "/var/lib/virtlet"
		config := &kubeapi.ContainerConfig{
			Image:  imageSpecs[ind],
			Mounts: []*kubeapi.Mount{{HostPath: &hostPath}},
			Labels: containers[ind].Labels,
		}
		containerIn := &kubeapi.CreateContainerRequest{
			PodSandboxId:  sandboxOut.PodSandboxId,
			Config:        config,
			SandboxConfig: sandbox,
		}

		createContainerOut, err := runtimeServiceClient.CreateContainer(context.Background(), containerIn)
		containers[ind].ContainerId = *createContainerOut.ContainerId

		if err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range filterTests {
		listContainersRequest := &kubeapi.ListContainersRequest{Filter: tc.containerFilter}

		listContainersOut, err := runtimeServiceClient.ListContainers(context.Background(), listContainersRequest)
		if err != nil {
			t.Fatal(err)
		}

		if len(listContainersOut.Containers) != len(tc.expectedIds) {
			t.Errorf("Expected %d sandboxes, instead got %d", len(tc.expectedIds), len(listContainersOut.Containers))
		}

		for _, id := range tc.expectedIds {
			found := false
			for _, container := range listContainersOut.Containers {
				if *container.Id == *id {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Didn't find expected sandbox id %s in returned containers list %v", *id, listContainersOut.Containers)
			}
		}
	}
}
