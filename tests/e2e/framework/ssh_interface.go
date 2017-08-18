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
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

type sshInterface struct {
	vmInterface *VMInterface
	client      *ssh.Client
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

	virtletPod, err := vmInterface.VirtletPod()
	if err != nil {
		return nil, err
	}
	container, err := virtletPod.Container("virtlet")
	if err != nil {
		return nil, err
	}

	client, server := net.Pipe()
	go func() {
		container.Exec([]string{"nc", "-q0", vmPod.Pod.Status.PodIP, "22"}, server, server, os.Stderr)
		client.Close()
	}()

	conn, chans, reqs, err := ssh.NewClientConn(client, fmt.Sprintf("%s.%s", vmPod.Pod.Name, vmPod.Pod.Namespace), config)
	if err != nil {
		return nil, err
	}

	sshClient := ssh.NewClient(conn, chans, reqs)
	return &sshInterface{
		vmInterface: vmInterface,
		client:      sshClient,
	}, nil
}

func (si *sshInterface) Exec(command []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	session, err := si.client.NewSession()
	if err != nil {
		return 0, err
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr
	session.Stdin = stdin

	exitcode := 0
	for i, arg := range command {
		if i == 0 {
			continue
		}
		command[i] = fmt.Sprintf("'%s'", strings.Replace(arg, "'", "\\'", -1))
	}
	err = session.Run(strings.Join(command, " "))
	if err != nil {
		if s, ok := err.(*ssh.ExitError); ok {
			exitcode = s.ExitStatus()
			err = nil
		}
	}
	if err != nil {
		return 0, err
	}
	return exitcode, nil
}

func (si *sshInterface) Close() error {
	return si.client.Close()
}
