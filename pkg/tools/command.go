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
	"io"
	"net/url"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"

	// register standard k8s types
	_ "k8s.io/client-go/pkg/api/install"
)

type ExecutorFactory func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error)

func defaultExecutorFactory(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
	return remotecommand.NewExecutor(config, method, url)
}

// VirtletCommand contains attributes and methods useful for all subcommands.
type VirtletCommand struct {
	executorFactory ExecutorFactory
	client          kubernetes.Interface
	restClient      rest.Interface
	config          *rest.Config
}

func (c *VirtletCommand) EnsureKubeClient() error {
	if c.executorFactory == nil {
		c.executorFactory = defaultExecutorFactory
	}
	if c.client != nil {
		return nil
	}
	config, client, err := getKubeClient()
	if err != nil {
		return err
	}
	c.client = client
	c.config = config
	c.restClient = client.CoreV1().RESTClient()
	return nil
}

// GetVirtletPodNames returns a list of names of the virtlet pods
// present in the cluster.
func (c *VirtletCommand) GetVirtletPodNames() ([]string, error) {
	if err := c.EnsureKubeClient(); err != nil {
		return nil, err
	}
	pods, err := c.client.CoreV1().Pods("kube-system").List(meta_v1.ListOptions{
		LabelSelector: "runtime=virtlet",
	})
	if err != nil {
		return nil, err
	}

	var r []string
	for _, item := range pods.Items {
		r = append(r, item.Name)
	}
	return r, nil
}

// ExecCommandOnContainer given a pod, container, namespace and command
// executes that command remotely returning stdout and stderr output
// as strings and an error if it occured.
// The specified stdin, stdout and stderr are used as the
// standard input / output / error streams of the remote command.
// No TTY is allocated by this function stdin.
func (c *VirtletCommand) ExecInContainer(
	pod, container, namespace string,
	stdin io.Reader, stdout, stderr io.Writer,
	command ...string,
) (int, error) {
	if err := c.EnsureKubeClient(); err != nil {
		return 0, err
	}
	req := c.restClient.Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&api.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     stdin != nil,
			Stdout:    stdout != nil,
			Stderr:    stderr != nil,
			TTY:       false,
		}, api.ParameterCodec)

	executor, err := c.executorFactory(c.config, "POST", req.URL())
	if err != nil {
		return 0, err
	}

	exitCode := 0
	err = executor.Stream(remotecommand.StreamOptions{
		SupportedProtocols: remotecommandconsts.SupportedStreamingProtocols,
		Stdin:              stdin,
		Stdout:             stdout,
		Stderr:             stderr,
	})

	if err != nil {
		if c, ok := err.(exec.CodeExitError); ok {
			exitCode = c.Code
			err = nil
		}
	}

	return exitCode, err
}
