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
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
	"k8s.io/kubernetes/pkg/api"

	"github.com/Mirantis/virtlet/pkg/tools"
)

// PodInterface provides API to work with a pod
type PodInterface struct {
	controller *Controller
	hasService bool

	Pod *v1.Pod
}

func newPodInterface(controller *Controller, pod *v1.Pod) *PodInterface {
	return &PodInterface{
		controller: controller,
		Pod:        pod,
	}
}

// Create creates pod in the k8s
func (pi *PodInterface) Create() error {
	updatedPod, err := pi.controller.client.Pods(pi.controller.Namespace()).Create(pi.Pod)
	if err != nil {
		return err
	}
	pi.Pod = updatedPod
	return nil
}

// Delete deletes the pod and associated service, which was earlier created by `controller.Run()`
func (pi *PodInterface) Delete() error {
	if pi.hasService {
		pi.controller.client.Services(pi.controller.Namespace()).Delete(pi.Pod.Name, nil)
	}
	return pi.controller.client.Pods(pi.controller.Namespace()).Delete(pi.Pod.Name, nil)
}

// Wait waits for pod to start and checks that it doesn't fail immediately after that
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

	return waitForConsistentState(func() error {
		podUpdated, err := pi.controller.client.Pods(pi.Pod.Namespace).Get(pi.Pod.Name, metav1.GetOptions{})
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

// WaitDestruction waits for the pod to be deleted
func (pi *PodInterface) WaitDestruction(timing ...time.Duration) error {
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

	return waitForConsistentState(func() error {
		_, err := pi.controller.client.Pods(pi.Pod.Namespace).Get(pi.Pod.Name, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("pod %s was not deleted", pi.Pod.Name)
	}, timeout, pollPeriond, consistencyPeriod)
}

// Container returns interface to execute commands in one of pod's containers
func (pi *PodInterface) Container(name string) (Executor, error) {
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
	return &containerInterface{
		podInterface: pi,
		name:         name,
	}, nil
}

// PortForward starts port forwarding to the specified ports to the specified pod
// in background. If a port entry has LocalPort = 0, it's updated with the real
// port number that was selected by the forwarder.
// Close returned channel to stop the port forwarder.
func (pi *PodInterface) PortForward(ports []*tools.ForwardedPort) (chan struct{}, error) {
	if len(ports) == 0 {
		return nil, errors.New("no ports specified")
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	stopCh := make(chan struct{})
	go func() {
		<-signals
		if stopCh != nil {
			close(stopCh)
		}
	}()

	restClient := pi.controller.client.RESTClient()
	req := restClient.Post().
		Resource("pods").
		Name(pi.Pod.Name).
		Namespace(pi.Pod.Namespace).
		SubResource("portforward")

	var buf bytes.Buffer
	var portsStr []string
	for _, p := range ports {
		portsStr = append(portsStr, p.String())
	}
	errCh := make(chan error, 1)
	readyCh := make(chan struct{})
	go func() {
		dialer, err := remotecommand.NewExecutor(pi.controller.restConfig, "POST", req.URL())
		if err != nil {
			errCh <- err
			return
		}
		fw, err := portforward.New(dialer, portsStr, stopCh, readyCh, &buf, os.Stderr)
		if err != nil {
			errCh <- err
			return
		}
		errCh <- fw.ForwardPorts()
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-readyCh:
		// FIXME: there appears to be no better way to get back the local ports as of now
		if err := tools.ParsePortForwardOutput(buf.String(), ports); err != nil {
			return nil, err
		}
	}
	return stopCh, nil

}

// DinDNodeExecutor return DinD executor for node, where this pod is located
func (pi *PodInterface) DinDNodeExecutor() (Executor, error) {
	return pi.controller.DinDNodeExecutor(pi.Pod.Spec.NodeName)
}

type containerInterface struct {
	podInterface *PodInterface
	name         string
}

// Run executes commands in one of containers in the pod
func (ci *containerInterface) Run(stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
	restClient := ci.podInterface.controller.client.RESTClient()
	req := restClient.Post().
		Resource("pods").
		Name(ci.podInterface.Pod.Name).
		Namespace(ci.podInterface.Pod.Namespace).
		SubResource("exec")
	req.VersionedParams(&api.PodExecOptions{
		Container: ci.name,
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
	}, api.ParameterCodec)

	executor, err := remotecommand.NewExecutor(ci.podInterface.controller.restConfig, "POST", req.URL())
	if err != nil {
		return err
	}

	options := remotecommand.StreamOptions{
		SupportedProtocols: remotecommandconsts.SupportedStreamingProtocols,
		Stdin:              stdin,
		Stdout:             stdout,
		Stderr:             stderr,
	}

	if err := executor.Stream(options); err != nil {
		if c, ok := err.(exec.CodeExitError); ok {
			return CommandError{ExitCode: c.Code}
		}
		return err
	}

	return nil
}

// Close closes the executor
func (*containerInterface) Close() error {
	return nil
}

// Start is a placeholder for fulfilling the Executor interface
func (*containerInterface) Start(stdin io.Reader, stdout, stderr io.Writer, command ...string) (Command, error) {
	return nil, errors.New("Not Implemented")
}
