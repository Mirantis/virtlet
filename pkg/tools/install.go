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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/renstrom/dedent"
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
)

const (
	pluginName = "virt"
)

// installCommand is used to install virtletctl as a kubectl plugin.
type installCommand struct {
	rootCmd        *cobra.Command
	executablePath string
	homeDir        string
}

// NewInstallCmd returns a cobra.Command that installs virtletctl as a kubectl plugin.
func NewInstallCmd(rootCmd *cobra.Command, executablePath, homeDir string) *cobra.Command {
	install := &installCommand{
		rootCmd:        rootCmd,
		executablePath: executablePath,
		homeDir:        homeDir,
	}
	return &cobra.Command{
		Use:   "install",
		Short: "Install virtletctl as a kubectl plugin",
		Long: dedent.Dedent(`
                        This command install virtletctl as a kubectl plugin.

                        After running this command, it becomes possible to run virtletctl
                        via 'kubectl plugin virt'.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("This command does not accept arguments")
			}
			return install.Run()
		},
	}
}

// Run executes the command.
func (inst *installCommand) Run() error {
	if inst.executablePath == "" {
		var err error
		inst.executablePath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("can't get executable path: %v", err)
		}
	}
	if inst.homeDir == "" {
		inst.homeDir = homedir.HomeDir()
	}
	oldUse := inst.rootCmd.Use
	inst.rootCmd.Use = pluginName
	defer func() {
		inst.rootCmd.Use = oldUse
	}()

	pluginDir := filepath.Join(inst.homeDir, ".kube", "plugins", inst.rootCmd.Name())
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("MkdirAll(): %v", err)
	}

	srcPath := filepath.Clean(inst.executablePath)
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("read opening virtletctl executable file %q: %v", srcPath, err)
	}
	defer src.Close()

	dstPath := filepath.Join(pluginDir, inst.rootCmd.Name())
	dst, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("error creating kubectl plugin binary file %q: %v", dstPath, err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("error copying virtletctl: %q -> %q: %v", srcPath, dstPath, err)
	}

	pluginYaml := pluginYamlFromCobraCommand(inst.rootCmd)
	pluginYamlPath := filepath.Join(pluginDir, "plugin.yaml")
	if err := ioutil.WriteFile(pluginYamlPath, pluginYaml, 0644); err != nil {
		return fmt.Errorf("error writing plugin.yaml: %v", err)
	}

	return nil
}
