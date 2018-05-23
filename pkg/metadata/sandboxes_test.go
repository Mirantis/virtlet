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
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/Mirantis/virtlet/pkg/metadata/fake"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

func TestRemovePodSandbox(t *testing.T) {
	sandboxes := fake.GetSandboxes(1)
	sandbox := sandboxes[0]

	tests := []struct {
		sandbox *types.PodSandboxConfig
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
		store, err := NewFakeStore()
		if err != nil {
			t.Fatal(err)
		}

		uid := ""
		if tc.sandbox != nil {
			uid = tc.sandbox.Uid
			psi, _ := NewPodSandboxInfo(tc.sandbox, nil, types.PodSandboxState_SANDBOX_READY, clockwork.NewRealClock())
			if err := store.PodSandbox(uid).Save(func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
				return psi, nil
			}); err != nil {
				t.Fatal(err)
			}
			dumpDB(t, store, "before delete")
		}
		if err := store.PodSandbox(uid).Save(func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
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
	sandboxes := fake.GetSandboxes(2)

	fakeClock := clockwork.NewFakeClockAt(time.Now())
	store := setUpTestStore(t, sandboxes, []*fake.ContainerTestConfig{}, fakeClock)

	for _, sandbox := range sandboxes {
		expectedSandboxInfo, err := NewPodSandboxInfo(sandbox, nil, types.PodSandboxState_SANDBOX_READY, fakeClock)
		if err != nil {
			t.Fatal(err)
		}
		if expectedSandboxInfo.PodID != "" {
			t.Error("podID must be empty for new PodSandboxInfo object")
		}
		sandboxManager := store.PodSandbox(sandbox.Uid)
		actualSandboxInfo, err := sandboxManager.Retrieve()
		if err != nil {
			t.Fatal(err)
		}
		if actualSandboxInfo == nil {
			t.Fatal(fmt.Errorf("missing PodSandboxInfo for sandbox %q", sandbox.Uid))
		}
		if actualSandboxInfo.PodID != sandboxManager.GetID() {
			t.Errorf("invalid podID for retrieved PodSandboxInfo: %s != %s", actualSandboxInfo.PodID, sandboxManager.GetID())
		}
		expectedSandboxInfo.PodID = sandboxManager.GetID()
		if !reflect.DeepEqual(expectedSandboxInfo, actualSandboxInfo) {
			t.Error("retrieved sandbox info object is not equal to expected value")
		}
	}
}

func TestSetGetPodSandboxStatus(t *testing.T) {
	sandboxes := fake.GetSandboxes(2)

	store := setUpTestStore(t, sandboxes, []*fake.ContainerTestConfig{}, nil)

	for _, sandbox := range sandboxes {
		sandboxInfo, err := store.PodSandbox(sandbox.Uid).Retrieve()
		if err != nil {
			t.Fatal(err)
		}
		if sandboxInfo == nil {
			t.Fatal(fmt.Errorf("missing PodSandboxInfo for sandbox %q", sandbox.Uid))
		}

		if sandboxInfo.State != types.PodSandboxState_SANDBOX_READY {
			t.Errorf("Sandbox state not ready")
		}

		if !reflect.DeepEqual(sandboxInfo.Config.Labels, sandbox.Labels) {
			t.Errorf("Expected %v, instead got %v", sandbox.Labels, sandboxInfo.Config.Labels)
		}

		if !reflect.DeepEqual(sandboxInfo.Config.Annotations, sandbox.Annotations) {
			t.Errorf("Expected %v, instead got %v", sandbox.Annotations, sandboxInfo.Config.Annotations)
		}

		if sandboxInfo.Config.Name != sandbox.Name {
			t.Errorf("Expected %s, instead got %s", sandbox.Name, sandboxInfo.Config.Name)
		}
	}
}

func TestListPodSandbox(t *testing.T) {
	genSandboxes := fake.GetSandboxes(2)

	firstSandboxConfig := genSandboxes[0]
	secondSandboxConfig := genSandboxes[1]

	firstSandboxConfig.Labels = map[string]string{"unique": "first", "common": "both"}
	secondSandboxConfig.Labels = map[string]string{"unique": "second", "common": "both"}

	sandboxConfigs := []*types.PodSandboxConfig{firstSandboxConfig, secondSandboxConfig}
	stateReady := types.PodSandboxState_SANDBOX_READY
	stateNotReady := types.PodSandboxState_SANDBOX_NOTREADY

	tests := []struct {
		filter      *types.PodSandboxFilter
		expectedIds []string
	}{
		{
			filter:      &types.PodSandboxFilter{},
			expectedIds: []string{firstSandboxConfig.Uid, secondSandboxConfig.Uid},
		},
		{
			filter: &types.PodSandboxFilter{
				Id: firstSandboxConfig.Uid,
			},
			expectedIds: []string{firstSandboxConfig.Uid},
		},
		{
			filter: &types.PodSandboxFilter{
				State: &stateReady,
			},
			expectedIds: []string{firstSandboxConfig.Uid, secondSandboxConfig.Uid},
		},
		{
			filter: &types.PodSandboxFilter{
				State: &stateNotReady,
			},
			expectedIds: []string{},
		},
		{
			filter: &types.PodSandboxFilter{
				LabelSelector: map[string]string{"unique": "first"},
			},
			expectedIds: []string{firstSandboxConfig.Uid},
		},
		{
			filter: &types.PodSandboxFilter{
				LabelSelector: map[string]string{"common": "both"},
			},
			expectedIds: []string{firstSandboxConfig.Uid, secondSandboxConfig.Uid},
		},
		{
			filter: &types.PodSandboxFilter{
				LabelSelector: map[string]string{"unique": "second", "common": "both"},
			},
			expectedIds: []string{secondSandboxConfig.Uid},
		},
		{
			filter: &types.PodSandboxFilter{
				Id:            firstSandboxConfig.Uid,
				LabelSelector: map[string]string{"unique": "second", "common": "both"},
			},
			expectedIds: []string{},
		},
		{
			filter: &types.PodSandboxFilter{
				Id:            firstSandboxConfig.Uid,
				LabelSelector: map[string]string{"unique": "first", "common": "both"},
			},
			expectedIds: []string{firstSandboxConfig.Uid},
		},
		{
			filter: &types.PodSandboxFilter{
				Id:            firstSandboxConfig.Uid,
				LabelSelector: map[string]string{"common": "both"},
			},
			expectedIds: []string{firstSandboxConfig.Uid},
		},
	}

	cc := fake.GetContainersConfig(sandboxConfigs)
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
				t.Errorf("Didn't find expected sandbox id %d in returned sandbox list %v", len(tc.expectedIds), sandboxes)
			}
		}
	}
}
