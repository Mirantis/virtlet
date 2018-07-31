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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"

	"github.com/Mirantis/virtlet/pkg/diag"
	"github.com/Mirantis/virtlet/pkg/version"
)

const (
	maxVirtletPodLogLines         = 20000
	sonobuoyPluginsConfigMapName  = "sonobuoy-plugins-cm"
	virtletSonobuoyPluginFileName = "virtlet.yaml"
	sonobuoyPluginYaml            = `sonobuoy-config:
  driver: Job
  plugin-name: virtlet
  result-type: virtlet
spec:
  command:
  - /bin/bash
  - -c
  - /sonobuoy.sh && sleep 3600
  env:
  - name: RESULTS_DIR
    value: /tmp/results
  image: mirantis/virtlet$TAG
  name: sonobuoy-virtlet
  volumeMounts:
  - mountPath: /tmp/results
    name: results
    readOnly: false
`
)

type diagDumpCommand struct {
	client  KubeClient
	out     io.Writer
	outDir  string
	useJSON bool
}

// NewDiagDumpCommand returns a new cobra.Command that dumps
// diagnostic information
func NewDiagDumpCommand(client KubeClient, out io.Writer) *cobra.Command {
	d := &diagDumpCommand{
		client: client,
		out:    out,
	}
	cmd := &cobra.Command{
		Use:   "dump output_dir",
		Short: "Dump Virtlet diagnostics information",
		Long:  "Pull Virtlet diagnostics information from the nodes and dump it as a directory tree or JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case !d.useJSON && len(args) != 1:
				return errors.New("Must specify output directory or --json")
			case !d.useJSON:
				d.outDir = args[0]
			case len(args) != 0:
				return errors.New("This command does not accept arguments")
			}
			return d.Run()
		},
	}
	cmd.Flags().BoolVar(&d.useJSON, "json", false, "Use JSON output")
	return cmd
}

func (d *diagDumpCommand) diagResult() (diag.Result, error) {
	dr := diag.Result{
		IsDir:    true,
		Name:     "nodes",
		Children: make(map[string]diag.Result),
	}
	podNames, nodeNames, err := d.client.GetVirtletPodAndNodeNames()
	if err != nil {
		return diag.Result{}, err
	}
	for n, podName := range podNames {
		nodeName := nodeNames[n]
		var buf bytes.Buffer
		exitCode, err := d.client.ExecInContainer(
			podName, "virtlet", "kube-system", nil,
			&buf, os.Stderr, []string{"virtlet", "--diag"})
		var cur diag.Result
		switch {
		case err != nil:
			cur = diag.Result{
				Ext:   "err",
				Error: fmt.Sprintf("node %q: error getting version from Virtlet pod %q: %v", nodeName, podName, err),
			}
		case exitCode != 0:
			cur = diag.Result{
				Ext:   "err",
				Error: fmt.Sprintf("node %q: error getting version from Virtlet pod %q: exit code %d", nodeName, podName, exitCode),
			}
		default:
			cur, err = diag.DecodeDiagnostics(buf.Bytes())
			if err != nil {
				cur = diag.Result{
					Ext:   "err",
					Error: fmt.Sprintf("error unmarshalling the diagnostics: %v", err),
				}
			}
		}
		if cur.IsDir {
			if sub, found := cur.Children["diagnostics"]; found && len(dr.Children) == 1 {
				cur = sub
			}
		}

		if cur.IsDir {
			d.dumpLogs(&cur, podName, "virtlet")
			d.dumpLogs(&cur, podName, "libvirt")
		}

		cur.Name = nodeName
		dr.Children[nodeName] = cur
	}
	return dr, nil
}

func (d *diagDumpCommand) dumpLogs(dr *diag.Result, podName, containerName string) {
	cur := diag.Result{
		Name: "virtlet-pod-" + containerName,
		Ext:  "log",
	}
	logs, err := d.client.PodLogs(podName, containerName, "kube-system", maxVirtletPodLogLines)
	if err != nil {
		cur.Error = err.Error()
	} else {
		cur.Data = string(logs)
	}
	dr.Children[cur.Name] = cur
}

func (d *diagDumpCommand) Run() error {
	dr, err := d.diagResult()
	if err != nil {
		return err
	}
	if d.useJSON {
		d.out.Write(dr.ToJSON())
		return nil
	}
	return dr.Unpack(d.outDir)
}

type diagUnpackCommand struct {
	in     io.Reader
	outDir string
}

// NewDiagUnpackCommand returns a new cobra.Command that unpacks
// diagnostic information
func NewDiagUnpackCommand(in io.Reader) *cobra.Command {
	d := &diagUnpackCommand{in: in}
	return &cobra.Command{
		Use:   "unpack output_dir",
		Short: "Unpack Virtlet diagnostics information",
		Long:  "Read Virtlet diagnostics information as JSON from stdin and unpacks into a directory tree",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("Must specify output directory")
			}
			d.outDir = args[0]
			return d.Run()
		},
	}
}

func (d *diagUnpackCommand) Run() error {
	data, err := ioutil.ReadAll(d.in)
	if err != nil {
		return err
	}
	dr, err := diag.DecodeDiagnostics(data)
	if err != nil {
		return err
	}
	return dr.Unpack(d.outDir)
}

type diagSonobuoyCommand struct {
	in  io.Reader
	out io.Writer
	tag string
}

// NewDiagSonobuoyCommand returns a new cobra.Command that adds
// Virtlet plugin to sonobuoy-generated yaml
func NewDiagSonobuoyCommand(in io.Reader, out io.Writer) *cobra.Command {
	d := &diagSonobuoyCommand{in: in, out: out}
	cmd := &cobra.Command{
		Use:   "sonobuoy",
		Short: "Add Virtlet sonobuoy plugin to the sonobuoy output",
		Long:  "Find and patch sonobuoy configmap in the yaml that's read from stdin to include Virtlet sonobuoy plugin",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("This command does not accept arguments")
			}
			return d.Run()
		},
	}
	cmd.Flags().StringVar(&d.tag, "tag", version.Get().ImageTag, "Set virtlet image tag for the plugin")
	return cmd
}

func (d *diagSonobuoyCommand) getYaml() ([]byte, error) {
	bs, err := ioutil.ReadAll(d.in)
	if err != nil {
		return nil, err
	}
	objs, err := LoadYaml(bs)
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, errors.New("source yaml is empty")
	}
	found := false
	for _, o := range objs {
		cfgMap, ok := o.(*v1.ConfigMap)
		if !ok {
			continue
		}
		if cfgMap.Name != sonobuoyPluginsConfigMapName {
			continue
		}
		found = true
		tagStr := ""
		if d.tag != "" {
			tagStr = ":" + d.tag
		}
		yaml := strings.Replace(sonobuoyPluginYaml, "$TAG", tagStr, -1)
		cfgMap.Data[virtletSonobuoyPluginFileName] = yaml
	}
	if !found {
		return nil, fmt.Errorf("ConfigMap not found: %q", sonobuoyPluginsConfigMapName)
	}
	return ToYaml(objs)
}

func (d *diagSonobuoyCommand) Run() error {
	bs, err := d.getYaml()
	if err != nil {
		return err
	}
	if _, err := d.out.Write(bs); err != nil {
		return err
	}
	return nil
}

// NewDiagCommand returns a new cobra.Command that handles Virtlet
// diagnostics.
func NewDiagCommand(client KubeClient, in io.Reader, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diag",
		Short: "Virtlet diagnostics",
		Long:  "Retrieve and unpack Virtlet diagnostics information",
	}
	cmd.AddCommand(NewDiagDumpCommand(client, out))
	cmd.AddCommand(NewDiagUnpackCommand(in))
	cmd.AddCommand(NewDiagSonobuoyCommand(in, out))
	return cmd
}
