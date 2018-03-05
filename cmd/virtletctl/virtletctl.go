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

package main

import (
	"os"

	"github.com/renstrom/dedent"
	"github.com/spf13/cobra"

	"github.com/Mirantis/virtlet/pkg/tools"
)

func newRootCmd() *cobra.Command {
	// rootCmd represents the base command when called without any subcommands
	cmd := &cobra.Command{
		Use:          "virtletctl",
		Short:        "Virtlet control tool",
		SilenceUsage: true,
		Long: dedent.Dedent(`
                        virtletctl provides a number of utilities for Virtet-enabled
                        Kubernetes cluster.`),
	}
	tools.AddGlobalFlags(cmd.PersistentFlags())
	client := tools.NewRealKubeClient()
	cmd.AddCommand(tools.NewDumpMetadataCmd(client))
	cmd.AddCommand(tools.NewVirshCmd(client, os.Stdout))
	cmd.AddCommand(tools.NewSshCmd(client, os.Stdout, ""))
	return cmd
}

func main() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
