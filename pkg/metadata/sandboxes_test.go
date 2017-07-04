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
	"reflect"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"

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
		store, err := NewFakeMetadataStore()
		if err != nil {
			t.Fatal(err)
		}

		uid := ""
		if tc.sandbox != nil {
			uid = tc.sandbox.GetMetadata().Uid
			psi, _ := NewPodSandboxInfo(tc.sandbox, []byte{}, kubeapi.PodSandboxState_SANDBOX_READY, clockwork.NewRealClock())
			if err := store.PodSandbox(uid).Save(func(c *PodSandboxInfo) (*PodSandboxInfo, error) {
				return psi, nil
			}); err != nil {
				t.Fatal(err)
			}
			dumpDB(t, store, "before delete")
		}
		if err := store.PodSandbox(uid).Save(func(c *PodSandboxInfo) (*PodSandboxInfo, error) {
			return nil, nil
		}); err != nil {
			if tc.error {

				continue
			}

			t.Fatal(err)
		} else {
			_, err = store.PodSandbox(uid).Retrieve()
			if err == nil {
				t.Error("Sandbox wasn't deleted")
			}
			dumpDB(t, store, "after delete")
		}
	}
}

func TestRetrieve(t *testing.T) {
	sandboxes := criapi.GetSandboxes(2)

	fakeClock := clockwork.NewFakeClockAt(time.Now())
	store := setUpTestStore(t, sandboxes, []*criapi.ContainerTestConfig{}, fakeClock)

	for _, sandbox := range sandboxes {
		expectedSandboxInfo, err := NewPodSandboxInfo(sandbox, "", kubeapi.PodSandboxState_SANDBOX_READY, fakeClock)
		if err != nil {
			t.Fatal(err)
		}
		if expectedSandboxInfo.podID != "" {
			t.Error("podID must be empty for new PodSandboxInfo object")
		}
		sandboxManager := store.PodSandbox(sandbox.GetMetadata().Uid)
		actualSandboxInfo, err := sandboxManager.Retrieve()
		if err != nil {
			t.Fatal(err)
		}
		if actualSandboxInfo.podID != sandboxManager.GetID() {
			t.Errorf("invalid podID for retrieved PodSandboxInfo: %s != %s", actualSandboxInfo.podID, sandboxManager.GetID())
		}
		expectedSandboxInfo.podID = sandboxManager.GetID()
		if !reflect.DeepEqual(expectedSandboxInfo, actualSandboxInfo) {
			t.Error("retrieved sandbox info object is not equal to expected value")
		}
	}
}

func TestSetGetPodSandboxStatus(t *testing.T) {
	sandboxes := criapi.GetSandboxes(2)

	store := setUpTestStore(t, sandboxes, []*criapi.ContainerTestConfig{}, nil)

	for _, sandbox := range sandboxes {
		sandboxInfo, err := store.PodSandbox(sandbox.GetMetadata().Uid).Retrieve()
		if err != nil {
			t.Fatal(err)
		}
		status := sandboxInfo.AsPodSandboxStatus()

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

	cc := criapi.GetContainersConfig(sandboxConfigs)
	b := setUpTestStore(t, sandboxConfigs, cc, nil)

	for _, tc := range tests {
		sandboxes, err := b.ListPodSandboxes(tc.filter)
		if err != nil {
			t.Fatal(err)
		}

		if len(sandboxes) != len(tc.expectedIds) {
			t.Errorf("Expected %d sandboxes, instead got %d", len(tc.expectedIds), len(sandboxes))
		}

		for _, id := range tc.expectedIds {
			found := false
			for _, podSandbox := range sandboxes {
				if id == podSandbox.GetID() {
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
