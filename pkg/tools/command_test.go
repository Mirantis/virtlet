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

	"github.com/spf13/cobra"
)

type fakeKubeClient struct {
	t                       *testing.T
	virtletPods             map[string]string
	vmPods                  map[string]VMPodInfo
	expectedCommands        map[string]string
	expectedPortForwards    []string
	portForwardStopChannels []chan struct{}
}

var _ KubeClient = &fakeKubeClient{}

func (c *fakeKubeClient) GetVirtletPodAndNodeNames() ([]string, []string, error) {
	var nodeNames []string
	for nodeName := range c.virtletPods {
		nodeNames = append(nodeNames, nodeName)
	}
	sort.Strings(nodeNames)

	var podNames []string
	for _, nodeName := range nodeNames {
		podNames = append(podNames, c.virtletPods[nodeName])
	}

	return podNames, nodeNames, nil
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

func (c *fakeKubeClient) ForwardPorts(podName, namespace string, ports []*ForwardedPort) (stopCh chan struct{}, err error) {
	var portStrs []string
	for n, p := range ports {
		portStrs = append(portStrs, p.String())
		if p.LocalPort == 0 {
			p.LocalPort = 4242 + uint16(n)
		}
	}
	if namespace == "" {
		namespace = "default"
	}
	key := fmt.Sprintf("%s/%s: %s", podName, namespace, strings.Join(portStrs, " "))
	var pfs []string
	found := false
	for _, pf := range c.expectedPortForwards {
		if pf == key {
			found = true
		} else {
			pfs = append(pfs, pf)
		}
	}
	if !found {
		c.t.Errorf("unexpected portforward: %q", key)
	}
	c.expectedPortForwards = pfs
	stopCh = make(chan struct{})
	c.portForwardStopChannels = append(c.portForwardStopChannels, stopCh)
	return stopCh, nil
}

func fakeCobraCommand() *cobra.Command {
	topCmd := &cobra.Command{
		Use:               "topcmd",
		Short:             "Topmost command",
		Long:              "Lorem ipsum dolor sit amet",
		DisableAutoGenTag: true,
	}
	var a string
	var b, c int
	topCmd.Flags().StringVarP(&a, "someflag", "f", "someflagvalue", "a flag")
	topCmd.Flags().IntVar(&b, "anotherflag", 42, "another flag")

	fooCmd := &cobra.Command{
		Use:     "foo",
		Short:   "Foo command",
		Long:    "Consectetur adipiscing elit",
		Example: "kubectl plugin topcmd foo",
		// make command "runnable" so gendocs works for it
		Run: func(*cobra.Command, []string) {},
	}
	fooCmd.Flags().IntVar(&c, "fooflag", 4242, "foo flag")
	topCmd.AddCommand(fooCmd)

	barCmd := &cobra.Command{
		Use:   "bar",
		Short: "Bar command",
		Long:  "Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua",
		Run:   func(*cobra.Command, []string) {},
	}
	topCmd.AddCommand(barCmd)

	return topCmd
}
