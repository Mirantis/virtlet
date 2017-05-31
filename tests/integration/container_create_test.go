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
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type containerFilterTestCase struct {
	name               string
	nilFilter          bool
	filterByPodSandbox bool
	filterByContainer  bool
	labelSelector      map[string]string
	expectedContainers []int
}

func (c *containerFilterTestCase) containerFilter(ct *containerTester) *kubeapi.ContainerFilter {
	if c.nilFilter {
		return nil
	}
	filter := &kubeapi.ContainerFilter{LabelSelector: c.labelSelector}
	if c.filterByPodSandbox {
		filter.PodSandboxId = ct.containers[0].SandboxId
	}
	if c.filterByContainer {
		filter.Id = ct.containers[0].ContainerId
	}
	return filter
}

func (c *containerFilterTestCase) expectedIds(ct *containerTester) []string {
	r := make([]string, len(c.expectedContainers))
	for n, idx := range c.expectedContainers {
		r[n] = ct.containers[idx].ContainerId
	}
	return r
}

func runShellCommand(t *testing.T, format string, args ...interface{}) string {
	command := fmt.Sprintf(format, args...)
	out, err := exec.Command("bash", "-c", command).Output()
	if err != nil {
		t.Fatalf("Error executing command '%q': %v", command, err)
	}
	return strings.TrimSpace(string(out))
}

func verifyUsingShell(t *testing.T, cmd, what, expected string) {
	t.Logf("Command to verify %s: %s", what, cmd)
	outStr := runShellCommand(t, "%s", cmd)
	if outStr != expected {
		t.Errorf("Verifying %s: expected %q, got %q", what, expected, outStr)
	}
}

func TestContainerVolumes(t *testing.T) {
	ct := newContainerTester(t)
	defer ct.teardown()
	ct.sandboxes[0].Annotations["VirtletVolumes"] = `[{"Name": "vol1"}, {"Name": "vol2", "Format": "qcow2", "Capacity": "2", "CapacityUnit": "MB"}, {"Name": "vol3"}]`
	ct.sandboxes[1].Annotations["VirtletVolumes"] = `[{"Name": "vol1", "Format": "qcow2", "CapacityUnit": "KB"}, {"Name": "vol2", "Capacity": "2"}]`
	ct.pullAllImages()

	for idx, sandbox := range ct.sandboxes {
		ct.runPodSandbox(sandbox)
		mounts := []*kubeapi.Mount{
			{
				HostPath: "/var/lib/virtlet",
			},
		}
		createResp := ct.createContainer(sandbox, ct.containers[idx], ct.imageSpecs[idx], mounts)
		ct.startContainer(createResp.ContainerId)

		vmName := createResp.ContainerId + "-" + ct.containers[idx].Name
		cmd := fmt.Sprintf("virsh domblklist '%s' | grep '%s-vol.*' | wc -l", vmName, createResp.ContainerId)
		count := 0
		if _, exists := sandbox.Annotations["VirtletVolumes"]; exists {
			count = len(strings.Split(sandbox.Annotations["VirtletVolumes"], "Name")) - 1
		}
		verifyUsingShell(t, cmd, "attached ephemeral volumes", strconv.Itoa(count))
	}

	if len(ct.listContainers(nil).Containers) != 2 {
		t.Errorf("expected 2 containers to be listed")
	}

	for _, container := range ct.containers {
		ct.stopContainer(container.ContainerId)
		ct.removeContainer(container.ContainerId)
		cmd := fmt.Sprintf("virsh vol-list --pool volumes | grep '%s' | wc -l", container.ContainerId)
		verifyUsingShell(t, cmd, "there's no attached ephemeral volumes", "0")
	}
}

func TestContainerCreateStartListRemove(t *testing.T) {
	ct := newContainerTester(t)
	defer ct.teardown()
	ct.containers[0].Labels = map[string]string{"unique": "first", "common": "both"}
	ct.containers[1].Labels = map[string]string{"unique": "second", "common": "both"}
	ct.pullAllImages()

	for idx, sandbox := range ct.sandboxes {
		ct.runPodSandbox(sandbox)
		createResp := ct.createContainer(sandbox, ct.containers[idx], ct.imageSpecs[idx], nil)
		ct.startContainer(createResp.ContainerId)
	}

	// Define external domain, i.e. not registered in bolt, to control virtlet performs well in that case
	if err := defineDummyDomain(); err != nil {
		t.Errorf("failed to define dummy domain to test List function: %v", err)
	}

	for _, tc := range []*containerFilterTestCase{
		{
			name:               "by container id",
			filterByContainer:  true,
			expectedContainers: []int{0},
		},
		{
			name:               "by sandbox id",
			filterByPodSandbox: true,
			expectedContainers: []int{0},
		},
		{
			name:               "by sandbox id and label selector",
			filterByPodSandbox: true,
			labelSelector:      map[string]string{"unique": "first", "common": "both"},
			expectedContainers: []int{0},
		},
		{
			name:               "by sandbox id and non-matching label selector",
			filterByPodSandbox: true,
			labelSelector:      map[string]string{"unique": "nomatch"},
			expectedContainers: []int{},
		},
		{
			name:               "by container id and sandbox id",
			filterByContainer:  true,
			filterByPodSandbox: true,
			expectedContainers: []int{0},
		},
		{
			name:               "by container id, sandbox id and label selector",
			filterByContainer:  true,
			filterByPodSandbox: true,
			labelSelector:      map[string]string{"unique": "first", "common": "both"},
			expectedContainers: []int{0},
		},
		{
			name:               "by label selector",
			labelSelector:      map[string]string{"unique": "first", "common": "both"},
			expectedContainers: []int{0},
		},
		{
			name:               "by label selector matching 2 containers",
			labelSelector:      map[string]string{"common": "both"},
			expectedContainers: []int{0, 1},
		},
		{
			name:               "by empty filter",
			expectedContainers: []int{0, 1},
		},
		{
			name:               "by nil filter",
			nilFilter:          true,
			expectedContainers: []int{0, 1},
		},
	} {
		listResp := ct.listContainers(tc.containerFilter(ct))
		expectedIds := tc.expectedIds(ct)
		actualIds := make([]string, len(listResp.Containers))
		for n, container := range listResp.Containers {
			actualIds[n] = container.Id
		}
		sort.Strings(expectedIds)
		sort.Strings(actualIds)
		expectedIdStr := strings.Join(expectedIds, ",")
		actualIdStr := strings.Join(actualIds, ",")
		if expectedIdStr != actualIdStr {
			t.Errorf("bad container list: %q instead of %q", actualIdStr, expectedIdStr)
		}
	}

	for _, container := range ct.containers {
		ct.stopContainer(container.ContainerId)
		ct.removeContainer(container.ContainerId)
	}

	if len(ct.listContainers(nil).Containers) != 0 {
		t.Errorf("expected no containers to be listed after removing them")
	}
}
