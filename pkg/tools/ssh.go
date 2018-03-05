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
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/renstrom/dedent"
	"github.com/spf13/cobra"
)

// sshCommand can be used to ssh into a VM pod.
type sshCommand struct {
	client        KubeClient
	user          string
	podName       string
	args          []string
	out           io.Writer
	sshExecutable string
}

// NewSshCmd returns a cobra.Command that performs ssh into a VM pod.
func NewSshCmd(client KubeClient, out io.Writer, sshExecutable string) *cobra.Command {
	ssh := &sshCommand{client: client, out: out, sshExecutable: sshExecutable}
	cmd := &cobra.Command{
		Use:   "ssh user@pod -- [ssh args...]",
		Short: "Connect to a VM pod using ssh",
		Long: dedent.Dedent(`
                        This command runs ssh and makes it connect to a VM pod.
                `),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("user name and pod name not specified")
			}
			parts := strings.Split(args[0], "@")
			if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) == 0 {
				return errors.New("malformed user@host")
			}
			ssh.user = parts[0]
			ssh.podName = parts[1]
			ssh.args = args[1:]
			if sshExecutable == "" {
				ssh.sshExecutable = "ssh"
			} else {
				ssh.sshExecutable = sshExecutable
			}
			return ssh.Run()
		},
	}
	return cmd
}

// Run executes the command.
func (s *sshCommand) Run() error {
	pf := &ForwardedPort{
		RemotePort: 22,
	}
	stopCh, err := s.client.ForwardPorts(s.podName, "", []*ForwardedPort{pf})
	if err != nil {
		return fmt.Errorf("error forwarding the ssh port: %v", err)
	}
	defer close(stopCh)
	sshArgs := append([]string{
		"-q",
		"-o",
		"StrictHostKeyChecking=no",
		"-o",
		"UserKnownHostsFile=/dev/null",
		"-p",
		strconv.Itoa(int(pf.LocalPort)),
		fmt.Sprintf("%s@127.0.0.1", s.user),
	}, s.args...)
	cmd := exec.Command(s.sshExecutable, sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = s.out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error executing ssh: %v", err)
	}
	return nil
}
