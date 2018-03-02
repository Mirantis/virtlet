/*
Copyright 2018 Mirantis

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

package tools

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
)

type fakeKubeClient struct {
	t                *testing.T
	virtletPods      map[string]string
	vmPods           map[string]VMPodInfo
	expectedCommands map[string]string
}

var _ KubeClient = &fakeKubeClient{}

func (c *fakeKubeClient) GetVirtletPodNames() ([]string, error) {
	var r []string
	for _, podName := range c.virtletPods {
		r = append(r, podName)
	}
	sort.Strings(r)
	return r, nil
}

func (c *fakeKubeClient) GetVirtletPodNameForNode(nodeName string) (string, error) {
	if podName, found := c.virtletPods[nodeName]; found {
		return podName, nil
	}
	return "", fmt.Errorf("no Virtlet pod on the node %q", nodeName)
}

func (c *fakeKubeClient) GetVMPodInfo(podName string) (*VMPodInfo, error) {
	if podInfo, found := c.vmPods[podName]; found {
		return &podInfo, nil
	}
	return nil, fmt.Errorf("VM pod not found: %q", podName)
}

func (c *fakeKubeClient) ExecInContainer(podName, containerName, namespace string,
	stdin io.Reader, stdout, stderr io.Writer,
	command []string) (int, error) {
	key := fmt.Sprintf("%s/%s/%s: %s", podName, containerName, namespace, strings.Join(command, " "))
	out, found := c.expectedCommands[key]
	if !found {
		c.t.Errorf("Unexpected command: %s", key)
		return 0, fmt.Errorf("unexpected command: %s", key)
	}
	delete(c.expectedCommands, key)
	if stdout != nil {
		if _, err := io.WriteString(stdout, out); err != nil {
			return 0, fmt.Errorf("WriteString(): %v", err)
		}
	}
	return 0, nil
}
