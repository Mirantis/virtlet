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
	"flag"
	"fmt"
	"os/user"
	"path/filepath"
	"strings"

	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig string
	master     string

	// map of all subcommands
	subcommands = map[string]SubCommand{
		"dump-metadata": DumpMetadata{},
	}
)

// SubCommand interface defines an interface for all subcommands.
type SubCommand interface {
	// RegisterFlags registers flags for subcommand
	RegisterFlags()
	// Run is main entry point to subcommand
	Run(clientset *typedv1.CoreV1Client, config *rest.Config, args []string) error
}

// ParseFlags registers additional flags for particular subcommand
// and then parse them with common one defined as vars in this package.
// It returns an error containing message with available commands if
// requested command was not recognized.
func ParseFlags(command string) error {
	flag.StringVar(&kubeconfig, "kubeconfig", "~/.kube/config", "absolute path to the kubeconfig file")
	flag.StringVar(&master, "master", "http://127.0.0.1:8080", "master url")

	if subcommand, err := getSubcommand(command); err != nil {
		return err
	} else {
		subcommand.RegisterFlags()
	}

	flag.Parse()

	return nil
}

// RunSubcommand creates kubernetes api client, passes it with args
// into subcommand and returns it's error if any.
// It returns an error if it can not create client or an error containing
// message with available commands if requested command was not recognized.
func RunSubcommand(command string, args []string) error {
	var subcommand SubCommand
	var err error
	if subcommand, err = getSubcommand(command); err != nil {
		return err
	}

	if kubeconfig[:2] == "~/" {
		usr, err := user.Current()
		if err != nil {
			return err
		}
		kubeconfig = filepath.Join(usr.HomeDir, kubeconfig[2:])
	}

	config, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		return fmt.Errorf("Can't create kubernetes api client config: %v", err)
	}

	client, err := typedv1.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("Can't create kubernetes api client: %v", err)
	}

	return subcommand.Run(client, config, args)
}

func getSubcommand(name string) (SubCommand, error) {
	if subcommand, ok := subcommands[name]; ok {
		return subcommand, nil
	}

	commands := make([]string, len(subcommands))
	i := 0
	for cmd := range subcommands {
		commands[i] = cmd
		i++
	}

	return nil, fmt.Errorf(
		"Subcommand %q unrecognized. Available commands: %s",
		name,
		strings.Join(commands, ", "),
	)
}
