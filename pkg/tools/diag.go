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

	"github.com/Mirantis/virtlet/pkg/diag"
	"github.com/spf13/cobra"
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

func (d *diagDumpCommand) Run() error {
	diagSet := diag.NewDiagSet()
	diagSet.RegisterDiagSource("nodes", &virtletNodeDiagSource{d.client})
	// TODO: combine virtletNodeDiagSource with "virtletPodLogs"
	// and "criproxy" source (support source combinations for this)
	dr := diagSet.RunDiagnostics().Children["nodes"]
	if d.useJSON {
		d.out.Write(dr.ToJSON())
		return nil
	}
	return dr.Unpack(d.outDir)
}

type virtletNodeDiagSource struct {
	client KubeClient
}

var _ diag.DiagSource = &virtletNodeDiagSource{}

func (s *virtletNodeDiagSource) DiagnosticInfo() (diag.DiagResult, error) {
	dr := diag.DiagResult{
		IsDir:    true,
		Children: make(map[string]diag.DiagResult),
	}
	podNames, nodeNames, err := s.client.GetVirtletPodAndNodeNames()
	if err != nil {
		return diag.DiagResult{}, err
	}
	for n, podName := range podNames {
		nodeName := nodeNames[n]
		var buf bytes.Buffer
		exitCode, err := s.client.ExecInContainer(
			podName, "virtlet", "kube-system", nil,
			&buf, os.Stderr, []string{"virtlet", "--diag"})
		var cur diag.DiagResult
		switch {
		case err != nil:
			cur = diag.DiagResult{
				Ext:   "err",
				Error: fmt.Sprintf("node %q: error getting version from Virtlet pod %q: %v", nodeName, podName, err),
			}
		case exitCode != 0:
			cur = diag.DiagResult{
				Ext:   "err",
				Error: fmt.Sprintf("node %q: error getting version from Virtlet pod %q: exit code %d", nodeName, podName, exitCode),
			}
		default:
			cur, err = diag.DecodeDiagnostics(buf.Bytes())
			if err != nil {
				cur = diag.DiagResult{
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

		cur.Name = nodeName
		dr.Children[nodeName] = cur
	}
	return dr, nil
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
	return cmd
}
