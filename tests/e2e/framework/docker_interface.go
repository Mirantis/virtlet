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
	"fmt"
	"io"
	"io/ioutil"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/filters"
	"github.com/docker/go-connections/nat"
)

// DockerContainerInterface is the receiver object for docker container operations
type DockerContainerInterface struct {
	client *client.Client
	Name   string
	ID     string
}

func newDockerContainerInterface(name string) (*DockerContainerInterface, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}
	return &DockerContainerInterface{
		client: cli,
		Name:   name,
	}, nil
}

// Run starts new docker container (similar to `docker run`)
func (d *DockerContainerInterface) Run(image string, env map[string]string, network string, ports []string, privileged bool, cmd ...string) error {
	var envLst []string
	for key, value := range env {
		envLst = append(envLst, fmt.Sprintf("%s=%s", key, value))
	}

	exposedPorts, portBindings, err := nat.ParsePortSpecs(ports)
	if err != nil {
		return err
	}
	config := &container.Config{
		ExposedPorts: exposedPorts,
		Env:          envLst,
		Image:        image,
		Cmd:          cmd,
	}

	hostConfig := &container.HostConfig{
		NetworkMode:  container.NetworkMode(network),
		PortBindings: portBindings,
		Privileged:   privileged,
	}

	ctx := context.Background()
	resp, err := d.client.ContainerCreate(ctx, config, hostConfig, nil, d.Name)
	if err != nil {
		return err
	}
	if err := d.client.ContainerStart(ctx, resp.ID); err != nil {
		return err
	}
	d.ID = resp.ID
	return nil
}

// PullImage pulls docker image from remote registry
func (d *DockerContainerInterface) PullImage(name string) error {
	out, err := d.client.ImagePull(context.Background(), name, types.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(ioutil.Discard, out)
	if err != nil {
		return err
	}
	return nil
}

// Delete deletes docker container
func (d *DockerContainerInterface) Delete() error {
	id := d.Name
	if d.ID != "" {
		id = d.ID
	}
	return d.client.ContainerRemove(context.Background(), id, types.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	})
}

// Container returns info for the container associated with method receiver
func (d *DockerContainerInterface) Container() (*types.Container, error) {
	args := filters.NewArgs()
	var id string
	if d.ID != "" {
		args.Add("id", d.ID)
		id = d.ID
	} else if d.Name != "" {
		args.Add("name", d.Name)
		id = d.Name
	} else {
		return nil, nil
	}
	containers, err := d.client.ContainerList(context.Background(), types.ContainerListOptions{
		All:    true,
		Filter: args,
	})
	if err != nil {
		return nil, err
	}
	if len(containers) < 1 {
		return nil, fmt.Errorf("Cannot find docker container %s", id)
	}
	return &containers[0], nil
}

// Executor returns interface to run commands in docker container
func (d *DockerContainerInterface) Executor(privileged bool, user string) Executor {
	return &DockerContainerExecInterface{
		user:            user,
		privileged:      privileged,
		dockerInterface: d,
	}
}

// DockerContainerExecInterface is the receiver object for commands execution in docker container
type DockerContainerExecInterface struct {
	dockerInterface *DockerContainerInterface
	user            string
	privileged      bool
}

var _ Executor = &DockerContainerExecInterface{}

// Run executes command in docker container
func (n *DockerContainerExecInterface) Run(stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
	ctx := context.Background()
	cfg := types.ExecConfig{
		AttachStdout: stdout != nil,
		AttachStderr: stderr != nil,
		AttachStdin:  stdin != nil,
		Cmd:          command,
		User:         n.user,
		Privileged:   n.privileged,
	}
	cr, err := n.dockerInterface.client.ContainerExecCreate(ctx, n.dockerInterface.Name, cfg)
	if err != nil {
		return err
	}

	r, err := n.dockerInterface.client.ContainerExecAttach(ctx, cr.ID, cfg)
	if err != nil {
		return err
	}
	err = containerHandleDataPiping(r, stdin, stdout, stderr)
	if err != nil {
		return err
	}

	info, err := n.dockerInterface.client.ContainerExecInspect(ctx, cr.ID)
	if err != nil {
		return err
	}

	if info.ExitCode != 0 {
		return CommandError{ExitCode: info.ExitCode}
	}

	return nil
}

// Close closes the executor
func (*DockerContainerExecInterface) Close() error {
	return nil
}

// Start is a placeholder for fulfilling Executor interface
func (*DockerContainerExecInterface) Start(stdin io.Reader, stdout, stderr io.Writer, command ...string) (Command, error) {
	return nil, fmt.Errorf("Not Implemented")
}

func containerHandleDataPiping(resp types.HijackedResponse, inStream io.Reader, outStream, errorStream io.Writer) error {
	var err error
	receiveStdout := make(chan error, 1)
	if outStream != nil || errorStream != nil {
		go func() {
			// Copy data from attached container session to both
			// out and error streams.
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
