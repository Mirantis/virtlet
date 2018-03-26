/*
Copyright 2017 Mirantis

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

package framework

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/Mirantis/virtlet/pkg/tools"
)

type sshInterface struct {
	vmInterface *VMInterface
	client      *ssh.Client
	fwStopCh    chan struct{}
}

func newSSHInterface(vmInterface *VMInterface, user, secret string) (*sshInterface, error) {
	var authMethod ssh.AuthMethod
	key := trimBlock(secret)
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		authMethod = ssh.Password(secret)
	} else {
		authMethod = ssh.PublicKeys(signer)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{authMethod},
	}

	vmPod, err := vmInterface.Pod()
	if err != nil {
		return nil, err
	}

	ports := []*tools.ForwardedPort{
		{RemotePort: 22},
	}
	fwStopCh, err := vmPod.PortForward(ports)
	if err != nil {
		return nil, err
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("localhost:%d", ports[0].LocalPort), config)
	if err != nil {
		defer close(fwStopCh)
		return nil, err
	}

	return &sshInterface{
		vmInterface: vmInterface,
		client:      sshClient,
		fwStopCh:    fwStopCh,
	}, nil
}

func (si *sshInterface) Run(stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
	cmd, err := si.Start(stdin, stdout, stderr, command...)
	if err != nil {
		return err
	}

	return cmd.Wait()
}

func (si *sshInterface) Start(stdin io.Reader, stdout, stderr io.Writer, command ...string) (Command, error) {
	session, err := si.client.NewSession()
	if err != nil {
		return nil, err
	}

	session.Stdout = stdout
	session.Stderr = stderr
	session.Stdin = stdin

	for i, arg := range command {
		if i == 0 {
			continue
		}
		command[i] = fmt.Sprintf("'%s'", strings.Replace(arg, "'", "\\'", -1))
	}
	if err := session.Start(strings.Join(command, " ")); err != nil {
		defer session.Close()
		return nil, err
	}

	return sshCommand{session: session}, err
}

func (si *sshInterface) Close() error {
	if si.client != nil {
		defer close(si.fwStopCh)
		err := si.client.Close()
		si.client = nil
		return err
	}
	return nil
}

type sshCommand struct {
	session *ssh.Session
}

var _ Command = &sshCommand{}

func (sc sshCommand) Wait() error {
	err := sc.session.Wait()
	defer sc.session.Close()
	if err != nil {
		if s, ok := err.(*ssh.ExitError); ok {
			return CommandError{ExitCode: s.ExitStatus()}
		}
		return err
	}

	return nil
}

func (sc sshCommand) Kill() error {
	defer sc.session.Close()
	return sc.session.Signal(ssh.SIGKILL)
}
