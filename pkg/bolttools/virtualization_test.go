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

	"github.com/boltdb/bolt"
)

func TestSetContainer(t *testing.T) {
	tests := []struct {
		containerId         string
		sandboxId           string
		image               string
		labels              map[string]string
		annotations         map[string]string
		expectedLabels      string
		expectedAnnotations string
	}{
		{
			containerId: "5d5fcd9d-c964-4db6-a86a-c92a6951275d",
			sandboxId:   "39f56df5-6c8a-44e3-8ae3-2736549637a9",
			image:       "cirros",
			labels: map[string]string{
				"foo":  "bar",
				"fizz": "buzz",
			},
			annotations: map[string]string{
				"fizz": "buzz",
				"virt": "let",
			},
			expectedLabels:      "{\"fizz\":\"buzz\",\"foo\":\"bar\"}",
			expectedAnnotations: "{\"fizz\":\"buzz\",\"virt\":\"let\"}",
		},
		{
			containerId: "a4ff0553-dbbd-4114-a9b6-8522eb66ef91",
			sandboxId:   "f9aa2197-2b77-4495-90f0-167963b07eb3",
			image:       "fedora",
			labels: map[string]string{
				"test":  "testing",
				"hello": "world",
			},
			annotations: map[string]string{
				"hello": "world",
				"test":  "testing",
			},
			expectedLabels:      "{\"hello\":\"world\",\"test\":\"testing\"}",
			expectedAnnotations: "{\"hello\":\"world\",\"test\":\"testing\"}",
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.SetContainer(tc.containerId, tc.sandboxId, tc.image, tc.labels, tc.annotations); err != nil {
			t.Fatal(err)
		}

		if err := b.db.View(func(tx *bolt.Tx) error {
			parentBucket := tx.Bucket([]byte("virtualization"))
			if parentBucket == nil {
				return fmt.Errorf("Bucket 'virtualization' doesn't exist")
			}

			bucket := parentBucket.Bucket([]byte(tc.containerId))
			if bucket == nil {
				return fmt.Errorf("Bucket '%s' doesn't exist", tc.containerId)
			}

			sandboxId, err := getString(bucket, "sandboxId")
			if err != nil {
				return err
			}

			if sandboxId != tc.sandboxId {
				t.Errorf("Expected %s, instead got %s", tc.sandboxId, sandboxId)
			}

			image, err := getString(bucket, "image")
			if err != nil {
				return err
			}

			if image != tc.image {
				t.Errorf("Expected %s, instead got %s", tc.image, image)
			}

			labels, err := getString(bucket, "labels")
			if err != nil {
				return err
			}

			if labels != tc.expectedLabels {
				t.Errorf("Expected %s, instead got %s", tc.expectedLabels, labels)
			}

			annotations, err := getString(bucket, "annotations")
			if err != nil {
				return err
			}

			if annotations != tc.expectedAnnotations {
				t.Errorf("Expected %s, instead got %s", tc.expectedAnnotations, annotations)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGetContainerInfo(t *testing.T) {
	tests := []struct {
		containerId string
		sandboxId   string
		image       string
		labels      map[string]string
		annotations map[string]string
	}{
		{
			containerId: "e05d74ac-6d22-406a-ab1c-5e99c0d6529f",
			sandboxId:   "92ac4008-bc31-457a-b598-735d9970515b",
			image:       "cirros",
			labels: map[string]string{
				"foo":  "bar",
				"fizz": "buzz",
			},
			annotations: map[string]string{
				"fizz": "buzz",
				"virt": "let",
			},
		},
		{
			containerId: "369f2409-8272-491a-8731-c7e4711ac93d",
			sandboxId:   "a417cbea-306a-41a4-aa3b-eae8d68e43ac",
			image:       "fedora",
			labels: map[string]string{
				"test":  "testing",
				"hello": "world",
			},
			annotations: map[string]string{
				"hello": "world",
				"test":  "testing",
			},
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.SetContainer(tc.containerId, tc.sandboxId, tc.image, tc.labels, tc.annotations); err != nil {
			t.Fatal(err)
		}

		containerInfo, err := b.GetContainerInfo(tc.containerId)
		if err != nil {
			t.Fatal(err)
		}

		if containerInfo.SandboxId != tc.sandboxId {
			t.Errorf("Expected %s, instead got %s", tc.sandboxId, containerInfo.SandboxId)
		}

		if containerInfo.Image != tc.image {
			t.Errorf("Expected %s, instead got %s", tc.image, containerInfo.Image)
		}

		if !reflect.DeepEqual(containerInfo.Labels, tc.labels) {
			t.Errorf("Expected %#v, instead got %#v", tc.labels, containerInfo.Labels)
		}

		if !reflect.DeepEqual(containerInfo.Annotations, tc.annotations) {
			t.Errorf("Expected %#v, instead got %#v", tc.annotations, containerInfo.Annotations)
		}
	}
}
