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
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	yaml "gopkg.in/yaml.v2"
)

// The Plugin / PluginFlag / Plugins types are based on
// pkg/kubectl/plugins/plugins.go from Kubernetes.

// Plugin holds everything needed to register a plugin as a
// command. Usually comes from a descriptor file.
type Plugin struct {
	// Name is the name of the plugin.
	Name string `yaml:"name"`
	// ShortDesc is the short description of the plugin.
	ShortDesc string `yaml:"shortDesc"`
	// LongDesc is the detailed description of the plugin.
	LongDesc string `yaml:"longDesc,omitempty"`
	// Example is an example of plugin usage.
	Example string `yaml:"example,omitempty"`
	// Command is the command that needs to be run by kubectl
	// to execute the plugin.
	Command string `yaml:"command,omitempty"`
	// Flags describes the flags for this plugin.
	Flags []PluginFlag `yaml:"flags,omitempty"`
	// Tree lists the subcommands of this plugin.
	Tree []*Plugin `yaml:"tree,omitempty"`
}

// PluginFlag describes a single flag supported by a given plugin.
type PluginFlag struct {
	// Name is the name of the flag
	Name string `yaml:"name"`
	// Shorthand is the shorthand for the flag, e.g. -o
	Shorthand string `yaml:"shorthand,omitempty"`
	// Desc is the description of the flag
	Desc string `yaml:"desc"`
	// DefValue is the default value of the flag.
	DefValue string `yaml:"defValue,omitempty"`
}

func pluginFromCobraCommand(cmd *cobra.Command) Plugin {
	p := Plugin{
		Name:      cmd.Name(),
		ShortDesc: cmd.Short,
		LongDesc:  cmd.Long,
		Example:   cmd.Example,
	}

	if cmds := cmd.Commands(); len(cmds) == 0 {
		// only process flags for commands w/o subcommands as
		// a cheap way of avoiding to process the global flags
		// that are passed from kubectl via env in any case
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden {
				p.Flags = append(p.Flags, PluginFlag{
					Name:      flag.Name,
					Shorthand: flag.Shorthand,
					Desc:      flag.Usage,
					DefValue:  flag.DefValue,
				})
			}
		})

		p.Command = "./" + cmd.CommandPath()
	} else {
		for _, c := range cmds {
			subPlugin := pluginFromCobraCommand(c)
			p.Tree = append(p.Tree, &subPlugin)
		}
	}

	return p
}

func pluginYamlFromCobraCommand(cmd *cobra.Command) []byte {
	out, err := yaml.Marshal(pluginFromCobraCommand(cmd))
	if err != nil {
		// this should never happen under the normal circumstances
		panic("plugin marshalling failed")
	}
	return out
}

// InPlugin returns true if virtletctl is running as a kubectl plugin.
func InPlugin() bool {
	_, inPlugin := os.LookupEnv("KUBECTL_PLUGINS_CALLER")
	return inPlugin
}

// SetLocalPluginFlags sets command flags from kubectl plugin
// environment variables in case if virtletctl is running as a kubectl
// plugin.
func SetLocalPluginFlags(cmd *cobra.Command) error {
	if !InPlugin() {
		return nil
	}
	var errs []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		v, found := os.LookupEnv(flagToEnvName(flag.Name, "KUBECTL_PLUGINS_LOCAL_FLAG_"))
		if !found {
			return
		}
		err := cmd.Flags().Set(flag.Name, v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", flag.Name, err))
		}
	})
	if len(errs) != 0 {
		return fmt.Errorf("errors parsing flags:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}
