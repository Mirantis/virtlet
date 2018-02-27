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

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	// "k8s.io/client-go/kubernetes/scheme"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	v1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
	//"k8s.io/kubernetes/pkg/api"
)

// SubCommandCommon contains attributes and methods useful for all subcommands.
type SubCommandCommon struct {
	client *typedv1.CoreV1Client
	config *rest.Config
}

// Setup prepares values for common between subcommands attributes
func (s *SubCommandCommon) Setup(client *typedv1.CoreV1Client, config *rest.Config) {
	s.client = client
	s.config = config
}

// GetVirtletPods returns a list of virtlet pod
func (s *SubCommandCommon) GetVirtletPods() ([]v1.Pod, error) {
	pods, err := s.client.Pods("kube-system").List(meta_v1.ListOptions{
		LabelSelector: "runtime=virtlet",
	})
	if err != nil {
		return nil, err
	}

	return pods.Items, nil
}

// ExecCommandOnContainer given a pod, container, namespace and command
// executes that command remotely returning stdout and stderr output
// as strings and error if any occured.
// If there is provided bytes.Buffer as stdin - it's content will be passed
// to remote command.
// Command is executed without a TTY as stdin.
func (s *SubCommandCommon) ExecCommandOnContainer(
	pod, container, namespace string,
	stdin io.Reader, stdout, stderr io.Writer,
	command ...string,
) (int, error) {

	req := s.client.RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		Param("container", container)

	if stdin != nil {
		req.Param("stdin", "true")
	}
	if stdout != nil {
		req.Param("stdout", "true")
	}
	if stderr != nil {
		req.Param("stderr", "true")
	}
	for _, cmd := range command {
		req.Param("command", cmd)
	}

	// Above replaces different below attempts which are producing incorrect
	// urls

	// req.VersionedParams(&scheme.PodExecOptions{
	// req.VersionedParams(&v1.PodExecOptions{
	// req.VersionedParams(&api.PodExecOptions{
	// 	Container: container,
	// 	Command: command,
	// 	Stdin: stdin != nil,
	// 	Stdout: stdout != nil,
	// 	Stderr: stderr != nil,
	// 	TTY: false,
	// }, api.ParameterCodec)
	// }, scheme.ParameterCodec)
	// fmt.Printf("Constructed url: %s\n", req.URL())

	executor, err := remotecommand.NewExecutor(s.config, "POST", req.URL())
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
