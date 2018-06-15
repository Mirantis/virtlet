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
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/Mirantis/virtlet/pkg/config"
)

// genDocCommand is used to generate markdown documentation for
// virtletctl commands.
type genDocCommand struct {
	rootCmd *cobra.Command
	out     io.Writer
	outDir  string
	config  bool
}

// NewGenDocCmd returns a cobra.Command that generates markdown
// documentation for virtletctl commands.
func NewGenDocCmd(rootCmd *cobra.Command, out io.Writer) *cobra.Command {
	gd := &genDocCommand{rootCmd: rootCmd, out: out}
	cmd := &cobra.Command{
		Use:   "gendoc output_dir",
		Short: "Generate Markdown documentation for the commands",
		Long:  "This command produces documentation for the whole command tree, or the Virtlet configuration data.",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case !gd.config && len(args) != 1:
				return errors.New("Must specify the output directory or --config")
			case !gd.config:
				gd.outDir = args[0]
			case len(args) != 0:
				return errors.New("Can't specify both the output directory and --config")
			}
			return gd.Run()
		},
	}
	cmd.Flags().BoolVar(&gd.config, "config", false, "Produce documentation for Virtlet config")
	return cmd
}

// Run executes the command.
func (gd *genDocCommand) Run() error {
	if gd.config {
		fmt.Fprint(gd.out, config.GenerateDoc())
	} else {
		return doc.GenMarkdownTree(gd.rootCmd, gd.outDir)
	}
	return nil
}
