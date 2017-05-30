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

package bolttools

import (
	"reflect"
	"testing"
	"time"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/tests/criapi"
)

func TestRemovePodSandbox(t *testing.T) {
	sandboxes := criapi.GetSandboxes(1)
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

		if err := b.EnsureSandboxSchema(); err != nil {
			t.Fatal(err)
		}

		uid := ""
		if tc.sandbox != nil {
			uid = tc.sandbox.GetMetadata().Uid
			if err := b.SetPodSandbox(tc.sandbox, []byte{}, time.Now); err != nil {
				t.Fatal(err)
			}
		}
		dumpDB(t, b.db)
		if err := b.RemovePodSandbox(uid); err != nil {
			if tc.error {
				continue
			}

			t.Fatal(err)
		}
	}
}

func TestSetGetPodSandboxStatus(t *testing.T) {
	sandboxes := criapi.GetSandboxes(2)

	b := SetUpBolt(t, sandboxes, []*criapi.ContainerTestConfig{})

	for _, sandbox := range sandboxes {
		status, err := b.GetPodSandboxStatus(sandbox.GetMetadata().Uid)
		if err != nil {
			t.Fatal(err)
		}

		if status.State != kubeapi.PodSandboxState_SANDBOX_READY {
			t.Errorf("Sandbox state not ready")
		}

		if !reflect.DeepEqual(status.GetLabels(), sandbox.GetLabels()) {
			t.Errorf("Expected %v, instead got %v", sandbox.GetLabels(), status.GetLabels())
		}

		if !reflect.DeepEqual(status.GetAnnotations(), sandbox.GetAnnotations()) {
			t.Errorf("Expected %v, instead got %v", sandbox.GetAnnotations(), status.GetAnnotations())
		}

		if status.GetMetadata().Name != sandbox.GetMetadata().Name {
			t.Errorf("Expected %s, instead got %s", sandbox.GetMetadata().Name, status.GetMetadata().Name)
		}
	}
}

func TestListPodSandbox(t *testing.T) {
	genSandboxes := criapi.GetSandboxes(2)

	firstSandboxConfig := genSandboxes[0]
	secondSandboxConfig := genSandboxes[1]

	firstSandboxConfig.Labels = map[string]string{"unique": "first", "common": "both"}
	secondSandboxConfig.Labels = map[string]string{"unique": "second", "common": "both"}

	sandboxConfigs := []*kubeapi.PodSandboxConfig{firstSandboxConfig, secondSandboxConfig}
	stateReady := kubeapi.PodSandboxState_SANDBOX_READY
	stateNotReady := kubeapi.PodSandboxState_SANDBOX_NOTREADY

	tests := []struct {
		filter      *kubeapi.PodSandboxFilter
		expectedIds []string
	}{
		{
			filter:      &kubeapi.PodSandboxFilter{},
			expectedIds: []string{firstSandboxConfig.Metadata.Uid, secondSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				Id: firstSandboxConfig.Metadata.Uid,
			},
			expectedIds: []string{firstSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				State: &kubeapi.PodSandboxStateValue{State: stateReady},
			},
			expectedIds: []string{firstSandboxConfig.Metadata.Uid, secondSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				State: &kubeapi.PodSandboxStateValue{State: stateNotReady},
			},
			expectedIds: []string{},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				LabelSelector: map[string]string{"unique": "first"},
			},
			expectedIds: []string{firstSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				LabelSelector: map[string]string{"common": "both"},
			},
			expectedIds: []string{firstSandboxConfig.Metadata.Uid, secondSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				LabelSelector: map[string]string{"unique": "second", "common": "both"},
			},
			expectedIds: []string{secondSandboxConfig.Metadata.Uid},
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
			expectedIds: []string{firstSandboxConfig.Metadata.Uid},
		},
		{
			filter: &kubeapi.PodSandboxFilter{
				Id:            firstSandboxConfig.Metadata.Uid,
				LabelSelector: map[string]string{"common": "both"},
			},
			expectedIds: []string{firstSandboxConfig.Metadata.Uid},
		},
	}

	b := SetUpBolt(t, sandboxConfigs, []*criapi.ContainerTestConfig{})

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
				if id == podSandbox.Id {
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
