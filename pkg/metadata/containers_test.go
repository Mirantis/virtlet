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

package metadata

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Mirantis/virtlet/tests/criapi"
)

func TestSetGetContainerInfo(t *testing.T) {
	sandboxes := criapi.GetSandboxes(2)
	containers := criapi.GetContainersConfig(sandboxes)

	store := setUpTestStore(t, sandboxes, containers, nil)

	for _, container := range containers {
		containerInfo, err := store.Container(container.ContainerId).Retrieve()
		if err != nil {
			t.Fatal(err)
		}
		if containerInfo == nil {
			t.Fatal(fmt.Errorf("containerInfo of container %s is not find in Virtlet metadata store", container.ContainerId))
		}

		if containerInfo.SandboxID != container.SandboxId {
			t.Errorf("Expected %s, instead got %s", container.SandboxId, containerInfo.SandboxID)
		}

		if containerInfo.Image != container.Image {
			t.Errorf("Expected %s, instead got %s", container.Image, containerInfo.Image)
		}

		if !reflect.DeepEqual(containerInfo.Labels, container.Labels) {
			t.Errorf("Expected %v, instead got %v", container.Labels, containerInfo.Labels)
		}

		if !reflect.DeepEqual(containerInfo.Annotations, container.Annotations) {
			t.Errorf("Expected %v, instead got %v", container.Annotations, containerInfo.Annotations)
		}
	}
}

func TestGetImagesInUse(t *testing.T) {
	sandboxes := criapi.GetSandboxes(2)
	containers := criapi.GetContainersConfig(sandboxes)

	store := setUpTestStore(t, sandboxes, containers, nil)

	expectedImagesInUse := map[string]bool{"testImage": true}
	imagesInUse, err := store.ImagesInUse()
	if err != nil {
		t.Fatalf("ImagesInUse(): %v", err)
	}
	if !reflect.DeepEqual(imagesInUse, expectedImagesInUse) {
		t.Errorf("bad result from ImagesInUse(): expected %#v, got #%v", expectedImagesInUse, imagesInUse)
	}
}

func TestRemoveContainer(t *testing.T) {
	sandboxes := criapi.GetSandboxes(2)
	containers := criapi.GetContainersConfig(sandboxes)

	store := setUpTestStore(t, sandboxes, containers, nil)

	for _, container := range containers {
		podContainers, err := store.ListPodContainers(container.SandboxId)
		if err != nil {
			t.Fatal(err)
		}
		if len(podContainers) != 1 || podContainers[0].GetID() != container.ContainerId {
			t.Errorf("Unexpected container list length: %d != 1", len(podContainers))
		}
		if err := store.Container(container.ContainerId).Save(func(c *ContainerInfo) (*ContainerInfo, error) {
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		podContainers, err = store.ListPodContainers(container.SandboxId)
		if err != nil {
			t.Fatal(err)
		}
		if len(podContainers) != 0 {
			t.Errorf("Unexpected container list length: %d != 0", len(podContainers))
		}
	}
}
