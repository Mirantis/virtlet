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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Mirantis/virtlet/pkg/version"
	"github.com/spf13/cobra"
)

type versionCommand struct {
	client     KubeClient
	out        io.Writer
	format     string
	short      bool
	clientOnly bool
	info       version.Info
}

// NewVersionCommand returns a cobra.Command that prints Virtlet
// version info
func NewVersionCommand(client KubeClient, out io.Writer, info *version.Info) *cobra.Command {
	v := &versionCommand{client: client, out: out}
	if info == nil {
		v.info = version.Get()
	} else {
		v.info = *info
	}
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Display Virtlet version information",
		Long:  "Display information about virtletctl version and Virtlet versions on the nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("This command does not accept arguments")
			}
			return v.Run()
		},
	}
	cmd.Flags().StringVarP(&v.format, "output", "o", "text", "One of 'text', 'short', 'yaml' or 'json'")
	cmd.Flags().BoolVar(&v.short, "short", false, "Print just the version number(s) (same as -o short)")
	cmd.Flags().BoolVar(&v.clientOnly, "client", false, "Print virtletctl version only")
	return cmd
}

func (v *versionCommand) getVersions() (version.ClusterVersionInfo, error) {
	vi := version.ClusterVersionInfo{ClientVersion: v.info}
	if v.clientOnly {
		return vi, nil
	}

	podNames, nodeNames, err := v.client.GetVirtletPodAndNodeNames()
	if err != nil {
		return vi, err
	}

	var errors []string
	for n, podName := range podNames {
		nodeName := nodeNames[n]
		var buf bytes.Buffer
		exitCode, err := v.client.ExecInContainer(
			podName, "virtlet", "kube-system",
			nil, &buf, os.Stderr,
			[]string{"virtlet", "-version", "-version-format", "json"})
		switch {
		case err != nil:
			errors = append(errors, fmt.Sprintf("node %q: error getting version from Virtlet pod %q: %v", nodeName, podName, err))
			continue
		case exitCode != 0:
			errors = append(errors, fmt.Sprintf("node %q: error getting version from Virtlet pod %q: exit code %d", nodeName, podName, exitCode))
			continue
		}
		var nv version.Info
		if err := json.Unmarshal(buf.Bytes(), &nv); err != nil {
			errors = append(errors, fmt.Sprintf("node %q: error unmarshalling version info from Virtlet pod %q: %v", nodeName, podName, err))
			continue
		}
		nv.NodeName = nodeName
		vi.NodeVersions = append(vi.NodeVersions, nv)
	}
	if !vi.AreNodesConsistent() {
		errors = append(errors, "some of the nodes have inconsistent Virtlet builds")
	}
	if len(errors) != 0 {
		return vi, fmt.Errorf("error encountered on some of the nodes:\n%s", strings.Join(errors, "\n"))
	}
	return vi, nil
}

func (v *versionCommand) Run() error {
	if v.short {
		v.format = "short"
	}
	vi, collectErr := v.getVersions()
	bs, err := vi.ToBytes(v.format)
	if err == nil {
		_, err = v.out.Write(bs)
	}
	if err != nil {
		return err
	}
	return collectErr
}
