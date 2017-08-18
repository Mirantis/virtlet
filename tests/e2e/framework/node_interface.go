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
	"context"
	"io"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
)

type NodeInterface struct {
	name       string
	user       string
	privileged bool
	client     *client.Client
}

func newNodeInterface(name string, privileged bool, user string) (*NodeInterface, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}
	return &NodeInterface{
		name:       name,
		user:       user,
		privileged: privileged,
		client:     cli,
	}, nil
}

func (n *NodeInterface) Exec(command []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cfg := types.ExecConfig{
		AttachStdout: stdout != nil,
		AttachStderr: stderr != nil,
		AttachStdin:  stdin != nil,
		Cmd:          command,
		User:         n.user,
		Privileged:   n.privileged,
	}
	cr, err := n.client.ContainerExecCreate(context.Background(), n.name, cfg)
	if err != nil {
		return 0, err
	}

	r, err := n.client.ContainerExecAttach(context.Background(), cr.ID, cfg)
	if err != nil {
		return 0, err
	}
	err = containerExecPipe(r, stdin, stdout, stderr)
	if err != nil {
		return 0, err
	}
	info, err := n.client.ContainerExecInspect(context.Background(), cr.ID)
	if err != nil {
		return 0, err
	}
	return info.ExitCode, nil
}

func (*NodeInterface) Close() error {
	return nil
}

func containerExecPipe(resp types.HijackedResponse, inStream io.Reader, outStream, errorStream io.Writer) error {
	var err error
	receiveStdout := make(chan error, 1)
	if outStream != nil || errorStream != nil {
		go func() {
			_, err = stdcopy.StdCopy(outStream, errorStream, resp.Reader)
			receiveStdout <- err
		}()
	}

	stdinDone := make(chan struct{})
	go func() {
		if inStream != nil {
			io.Copy(resp.Conn, inStream)
		}

		resp.CloseWrite()
		close(stdinDone)
	}()

	select {
	case err := <-receiveStdout:
		if err != nil {
			return err
		}
	case <-stdinDone:
		if outStream != nil || errorStream != nil {
			if err := <-receiveStdout; err != nil {
				return err
			}
		}
	}

	return nil
}
