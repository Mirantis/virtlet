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
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"github.com/Mirantis/virtlet/tests/criapi"
)

func TestSetSandBoxValidation(t *testing.T) {
	invalidSandboxes, err := criapi.GetSandboxes(3)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}

	//Now let's make generated configs to be invalid
	invalidSandboxes[0].Metadata = nil
	invalidSandboxes[1].Linux = nil
	invalidSandboxes[2].Linux.NamespaceOptions = nil

	b, err := NewFakeBoltClient()
	if err != nil {
		t.Fatal(err)
	}

	if err := b.VerifySandboxSchema(); err != nil {
		t.Fatal(err)
	}

	for _, sandbox := range invalidSandboxes {
		if sandbox != nil {
			if err := b.SetPodSandbox(sandbox, []byte{}); err == nil {
				t.Fatalf("Expected to recieve error on attempt to set invalid sandbox %v", sandbox)
			}
		}
	}
}

func TestSetPodSandbox(t *testing.T) {
	sandboxes, err := criapi.GetSandboxes(2)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}

	b := SetUpBolt(t, sandboxes, []*criapi.ContainerTestConfigSet{})

	for _, sandbox := range sandboxes {
		if err := b.db.View(func(tx *bolt.Tx) error {
			parentBucket := tx.Bucket([]byte("sandbox"))
			if parentBucket == nil {
				return fmt.Errorf("bucket 'sandbox' doesn't exist")
			}

			bucket := parentBucket.Bucket([]byte(sandbox.GetMetadata().GetUid()))
			if bucket == nil {
				return fmt.Errorf("bucket '%s' doesn't exist", sandbox.GetMetadata().GetUid())
			}

			hostname, err := getString(bucket, "hostname")
			if err != nil {
				return err
			}
			if hostname != sandbox.GetHostname() {
				t.Errorf("Expected %s, instead got %s", sandbox.GetHostname(), hostname)
			}

			strLabels, err := getString(bucket, "labels")
			if err != nil {
				return err
			}

			matchJson, err := json.Marshal(sandbox.GetLabels())
			if err != nil {
				return err
			}

			if strLabels != string(matchJson) {
				t.Errorf("Expected %s, instead got %s", matchJson, strLabels)
			}

			matchJson, err = json.Marshal(sandbox.GetAnnotations())
			if err != nil {
				return err
			}

			strAnnotations, err := getString(bucket, "annotations")
			if err != nil {
				return err
			}

			if strAnnotations != string(matchJson) {
				t.Errorf("Expected %s, instead got %s", matchJson, strAnnotations)
			}

			metadataBucket := bucket.Bucket([]byte("metadata"))
			if metadataBucket == nil {
				return fmt.Errorf("bucket 'metadata' doesn't exist")
			}

			name, err := getString(metadataBucket, "name")
			if err != nil {
				return err
			}
			if name != sandbox.GetMetadata().GetName() {
				t.Errorf("Expected %s, instead got %s", sandbox.GetMetadata().GetName(), name)
			}

			uid, err := getString(metadataBucket, "uid")
			if err != nil {
				return err
			}
			if uid != sandbox.GetMetadata().GetUid() {
				t.Errorf("Expected %s, instead got %s", sandbox.GetMetadata().GetUid(), uid)
			}

			namespace, err := getString(metadataBucket, "namespace")
			if err != nil {
				return err
			}
			if namespace != sandbox.GetMetadata().GetNamespace() {
				t.Errorf("Expected %s, instead got %s", sandbox.GetMetadata().GetNamespace(), namespace)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRemovePodSandbox(t *testing.T) {
	sandboxes, err := criapi.GetSandboxes(1)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}

	sandbox := sandboxes[0]

	tests := []struct {
		sandbox *kubeapi.PodSandboxConfig
		error   bool
	}{
		{
			sandbox: sandbox,
			error:   false,
		},
		{
			sandbox: nil,
			error:   true,
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.VerifySandboxSchema(); err != nil {
			t.Fatal(err)
		}

		if tc.sandbox != nil {
			if err := b.SetPodSandbox(tc.sandbox, []byte{}); err != nil {
				t.Fatal(err)
			}
		}
		dumpDB(t, b.db)
		if err := b.RemovePodSandbox(tc.sandbox.GetMetadata().GetUid()); err != nil {
			if tc.error {
				continue
			}

			t.Fatal(err)
		}
	}
}

func TestGetPodSandboxStatus(t *testing.T) {
	sandboxes, err := criapi.GetSandboxes(2)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}

	b := SetUpBolt(t, sandboxes, []*criapi.ContainerTestConfigSet{})

	for _, sandbox := range sandboxes {
		status, err := b.GetPodSandboxStatus(sandbox.GetMetadata().GetUid())
		if err != nil {
			t.Fatal(err)
		}

		if status.GetState() != kubeapi.PodSandBoxState_READY {
			t.Errorf("Sandbox state not ready")
		}

		if !reflect.DeepEqual(status.GetLabels(), sandbox.GetLabels()) {
			t.Errorf("Expected %v, instead got %v", sandbox.GetLabels(), status.GetLabels())
		}

		if !reflect.DeepEqual(status.GetAnnotations(), sandbox.GetAnnotations()) {
			t.Errorf("Expected %v, instead got %v", sandbox.GetAnnotations(), status.GetAnnotations())
		}

		if status.GetMetadata().GetName() != sandbox.GetMetadata().GetName() {
			t.Errorf("Expected %s, instead got %s", sandbox.GetMetadata().GetName(), status.GetMetadata().GetName())
		}
	}
}

func TestListPodSandbox(t *testing.T) {
	genSandboxes, err := criapi.GetSandboxes(2)
	if err != nil {
		t.Fatalf("Failed to generate array of sandbox configs: %v", err)
	}

	firstSandboxConfig := genSandboxes[0]
	secondSandboxConfig := genSandboxes[1]

	firstSandboxConfig.Labels = map[string]string{"unique": "first", "common": "both"}
	secondSandboxConfig.Labels = map[string]string{"unique": "second", "common": "both"}

	sandboxConfigs := []*kubeapi.PodSandboxConfig{firstSandboxConfig, secondSandboxConfig}
	state_ready := kubeapi.PodSandBoxState_READY
	state_notready := kubeapi.PodSandBoxState_NOTREADY

	tests := []struct {
		filter      *kubeapi.PodSandboxFilter
		expectedIds []string
	}{
		{
			filter:      &kubeapi.PodSandboxFilter{},
			expectedIds: []string{*firstSandboxConfig.Metadata.Uid, *secondSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				Id: firstSandboxConfig.Metadata.Uid,
			},
			expectedIds: []string{*firstSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				State: &state_ready,
			},
			expectedIds: []string{*firstSandboxConfig.Metadata.Uid, *secondSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				State: &state_notready,
			},
			expectedIds: []string{},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				LabelSelector: map[string]string{"unique": "first"},
			},
			expectedIds: []string{*firstSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				LabelSelector: map[string]string{"common": "both"},
			},
			expectedIds: []string{*firstSandboxConfig.Metadata.Uid, *secondSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				LabelSelector: map[string]string{"unique": "second", "common": "both"},
			},
			expectedIds: []string{*secondSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				Id:            firstSandboxConfig.Metadata.Uid,
				LabelSelector: map[string]string{"unique": "second", "common": "both"},
			},
			expectedIds: []string{},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				Id:            firstSandboxConfig.Metadata.Uid,
				LabelSelector: map[string]string{"unique": "first", "common": "both"},
			},
			expectedIds: []string{*firstSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				Id:            firstSandboxConfig.Metadata.Uid,
				LabelSelector: map[string]string{"common": "both"},
			},
			expectedIds: []string{*firstSandboxConfig.Metadata.Uid},
		},
	}

	b := SetUpBolt(t, sandboxConfigs, []*criapi.ContainerTestConfigSet{})

	for _, tc := range tests {
		sandboxes, err := b.ListPodSandbox(tc.filter)
		if err != nil {
			t.Fatal(err)
		}

		if len(sandboxes) != len(tc.expectedIds) {
			t.Errorf("Expected %d sandboxes, instead got %d", len(tc.expectedIds), len(sandboxes))
		}

		for _, id := range tc.expectedIds {
			found := false
			for _, podSandbox := range sandboxes {
				if id == *podSandbox.Id {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Didn't find expected sandbox id %s in returned sandbox list %v", len(tc.expectedIds), sandboxes)
			}
		}
	}
}
