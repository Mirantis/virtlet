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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
	"k8s.io/kubernetes/pkg/api"
)

type PodInterface struct {
	controller *Controller

	Pod *v1.Pod
}

func newPodInterface(controller *Controller, pod *v1.Pod) *PodInterface {
	return &PodInterface{
		controller: controller,
		Pod:        pod,
	}
}

func (pi *PodInterface) Create() error {
	updatedPod, err := pi.controller.Client.Pods(pi.controller.Namespace.Name).Create(pi.Pod)
	if err != nil {
		return err
	}
	pi.Pod = updatedPod
	return nil
}

func (pi *PodInterface) Wait(timing ...time.Duration) error {
	timeout := time.Minute * 5
	pollPeriond := time.Second
	consistencyPeriod := time.Second * 5
	if len(timing) > 0 {
		timeout = timing[0]
	}
	if len(timing) > 1 {
		pollPeriond = timing[1]
	}
	if len(timing) > 2 {
		consistencyPeriod = timing[2]
	}

	return waitConsistentState(func() error {
		podUpdated, err := pi.controller.Client.Pods(pi.Pod.Namespace).Get(pi.Pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		pi.Pod = podUpdated
		phase := v1.PodRunning
		if podUpdated.Status.Phase != phase {
			return fmt.Errorf("pod %s is not %s phase: %s", podUpdated.Name, phase, podUpdated.Status.Phase)
		}
		return nil
	}, timeout, pollPeriond, consistencyPeriod)
}

func (pi *PodInterface) Container(name string) (*ContainerInterface, error) {
	if name == "" && len(pi.Pod.Spec.Containers) > 0 {
		name = pi.Pod.Spec.Containers[0].Name
	}
	found := false
	for _, c := range pi.Pod.Spec.Containers {
		if c.Name == name {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("container %s doesn't exist in pod %s in namespace %s", name, pi.Pod.Name, pi.Pod.Namespace)
	}
	return &ContainerInterface{
		podInterface: pi,
		Name:         name,
	}, nil
}

type ContainerInterface struct {
	podInterface *PodInterface

	Name string
}

func (ci *ContainerInterface) Exec(command []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	restClient := ci.podInterface.controller.Client.RESTClient()
	req := restClient.Post().
		Resource("pods").
		Name(ci.podInterface.Pod.Name).
		Namespace(ci.podInterface.Pod.Namespace).
		SubResource("exec")
	req.VersionedParams(&api.PodExecOptions{
		Container: ci.Name,
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
	}, api.ParameterCodec)

	executor, err := remotecommand.NewExecutor(ci.podInterface.controller.RestConfig, "POST", req.URL())
	if err != nil {
		return 0, err
	}

	exitCode := 0
	options := remotecommand.StreamOptions{
		SupportedProtocols: remotecommandconsts.SupportedStreamingProtocols,
		Stdin:              stdin,
		Stdout:             stdout,
		Stderr:             stderr,
	}
	err = executor.Stream(options)
	if err != nil {
		if c, ok := err.(exec.CodeExitError); ok {
			exitCode = c.Code
			err = nil
		}
	}
	if err != nil {
		return 0, err
	}
	return exitCode, nil
}

func (*ContainerInterface) Close() error {
	return nil
}
