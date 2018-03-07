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
	"flag"
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

	clientCfg := tools.BindFlags(cmd.PersistentFlags())
	// Fix unwanted glog warnings, see
	// https://github.com/kubernetes/kubernetes/issues/17162#issuecomment-225596212
	flag.CommandLine.Parse([]string{})

	client := tools.NewRealKubeClient(clientCfg)
	cmd.AddCommand(tools.NewDumpMetadataCmd(client))
	cmd.AddCommand(tools.NewVirshCmd(client, os.Stdout))
	cmd.AddCommand(tools.NewSshCmd(client, os.Stdout, ""))
	cmd.AddCommand(tools.NewInstallCmd(cmd, "", ""))
	cmd.AddCommand(tools.NewGenDocCmd(cmd))

	for _, c := range cmd.Commands() {
		c.PreRunE = func(*cobra.Command, []string) error {
			return tools.SetLocalPluginFlags(c)
		}
	}

	return cmd
}

func main() {
	cmd := newRootCmd()
	if tools.InPlugin() && len(os.Args) > 1 {
		// in case of a kubectl plugin, all the options are
		// already removed from the command line
		args := []string{os.Args[1]}
		args = append(args, "--")
		args = append(args, os.Args[2:]...)
		cmd.SetArgs(args)
	}
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
