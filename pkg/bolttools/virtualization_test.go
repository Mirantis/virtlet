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

package bolttools

import (
	"fmt"
	"reflect"
	"testing"

	"encoding/json"
	"github.com/boltdb/bolt"
	"github.com/Mirantis/virtlet/tests/criapi"
)

func TestSetContainer(t *testing.T) {
	sandboxes, err := criapi.GetSandboxes(2)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}
	containers, err := criapi.GetContainersConfig(sandboxes)
	if err != nil {
		t.Fatalf("Failed to generate array of container configs: %v", err)
	}

	b := SetUpBolt(t, sandboxes, containers)

	for _, container := range containers {

		if err := b.db.View(func(tx *bolt.Tx) error {
			parentBucket := tx.Bucket([]byte("virtualization"))
			if parentBucket == nil {
				return fmt.Errorf("bucket 'virtualization' doesn't exist")
			}

			bucket := parentBucket.Bucket([]byte(container.ContainerId))
			if bucket == nil {
				return fmt.Errorf("bucket '%s' doesn't exist", container.ContainerId)
			}

			sandboxId, err := getString(bucket, "sandboxId")
			if err != nil {
				return err
			}

			if sandboxId != container.SandboxId {
				t.Errorf("Expected %s, instead got %s", container.SandboxId, sandboxId)
			}

			image, err := getString(bucket, "image")
			if err != nil {
				return err
			}

			if image != container.Image {
				t.Errorf("Expected %s, instead got %s", container.Image, image)
			}

			labels, err := getString(bucket, "labels")
			if err != nil {
				return err
			}

			matchJson, err := json.Marshal(container.Labels)
			if err != nil {
				return err
			}

			if labels != string(matchJson) {
				t.Errorf("Expected %s, instead got %s", matchJson, labels)
			}

			annotations, err := getString(bucket, "annotations")
			if err != nil {
				return err
			}

			matchJson, err = json.Marshal(container.Annotations)
			if err != nil {
				return err
			}

			if annotations != string(matchJson) {
				t.Errorf("Expected %s, instead got %s", matchJson, annotations)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		contID, err := b.GetPodSandboxContainerID(container.SandboxId)
		if err != nil {
			t.Fatal(err)
		}
		if contID != container.ContainerId {
			t.Errorf("Expected to get containerID: '%s' in ContainerID field: '%s' of PodSandbox:'%s'", container.ContainerId, contID, container.SandboxId)
		}
	}
}

func TestGetContainerInfo(t *testing.T) {
	sandboxes, err := criapi.GetSandboxes(2)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}
	containers, err := criapi.GetContainersConfig(sandboxes)
	if err != nil {
		t.Fatalf("Failed to generate array of container configs: %v", err)
	}

	b := SetUpBolt(t, sandboxes, containers)

	for _, container := range containers {
		containerInfo, err := b.GetContainerInfo(container.ContainerId)
		if err != nil {
			t.Fatal(err)
		}

		if containerInfo.SandboxId != container.SandboxId {
			t.Errorf("Expected %s, instead got %s", container.SandboxId, containerInfo.SandboxId)
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

func TestRemoveContainer(t *testing.T) {
	sandboxes, err := criapi.GetSandboxes(2)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}
	containers, err := criapi.GetContainersConfig(sandboxes)
	if err != nil {
		t.Fatalf("Failed to generate array of container configs: %v", err)
	}

	b := SetUpBolt(t, sandboxes, containers)

	for _, container := range containers {
		contID, err := b.GetPodSandboxContainerID(container.SandboxId)
		if err != nil {
			t.Fatal(err)
		}
		if contID != container.ContainerId {
			t.Errorf("Expected to get containerID: '%s' in ContainerID field: '%s' of PodSandbox:'%s'", container.ContainerId, contID, container.SandboxId)
		}
		if err := b.RemoveContainer(container.ContainerId); err != nil {
			t.Fatal(err)
		}
		contID, err = b.GetPodSandboxContainerID(container.SandboxId)
		if err != nil {
			t.Fatal(err)
		}
		if contID != "" {
			t.Errorf("Expected to have empty string in ContainerID of PodSandbox after removing of container with id: '%s' but have:'%v'", container.ContainerId, contID)
		}
	}
}
