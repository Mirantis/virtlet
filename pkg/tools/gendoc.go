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

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// genDocCommand is used to generate markdown documentation for
// virtletctl commands.
type genDocCommand struct {
	rootCmd *cobra.Command
	outDir  string
}

func NewGenDocCmd(rootCmd *cobra.Command) *cobra.Command {
	gd := &genDocCommand{rootCmd: rootCmd}
	return &cobra.Command{
		Use:   "gendoc output_dir",
		Short: "Generate Markdown documentation for the commands",
		Long:  "This command produces documentation for the whole command tree.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("Must specify the output directory")
			}
			gd.outDir = args[0]
			return gd.Run()
		},
	}
}

// Run executes the command.
func (gd *genDocCommand) Run() error {
	return doc.GenMarkdownTree(gd.rootCmd, gd.outDir)
}
