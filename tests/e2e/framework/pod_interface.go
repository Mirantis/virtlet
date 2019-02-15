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
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/exec"

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
		pi.controller.client.Services(pi.Pod.Namespace).Delete(pi.Pod.Name, nil)
	}
	return pi.controller.client.Pods(pi.Pod.Namespace).Delete(pi.Pod.Name, nil)
}

// WaitForPodStatus waits for the pod to reach the specified status. If expectedContainerErrors
// is empty, the pod is expected to become Running and Ready. If it isn't, the pod is expected
// to have one of these errors among its container statuses.
func (pi *PodInterface) WaitForPodStatus(expectedContainerErrors []string, timing ...time.Duration) error {
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

		needErrors := len(expectedContainerErrors) > 0
		phase := v1.PodRunning
		if needErrors {
			phase = v1.PodPending
		}
		if podUpdated.Status.Phase != phase {
			return fmt.Errorf("pod %s is not %s phase: %s", podUpdated.Name, phase, podUpdated.Status.Phase)
		}

		gotExpectedError := false
		for _, cs := range podUpdated.Status.ContainerStatuses {
			switch {
			case !needErrors && cs.State.Running == nil:
				return fmt.Errorf("container %s in pod %s is not running: %s", cs.Name, podUpdated.Name, spew.Sdump(cs.State))
			case !needErrors && !cs.Ready:
				return fmt.Errorf("container %s in pod %s did not passed its readiness probe", cs.Name, podUpdated.Name)
			case needErrors && cs.State.Waiting == nil:
				return fmt.Errorf("container %s in pod %s not in waiting state", cs.Name, podUpdated.Name)
			case needErrors:
				for _, errStr := range expectedContainerErrors {
					if cs.State.Waiting.Reason == errStr {
						gotExpectedError = true
						break
					}
				}
			}
		}
		if needErrors && !gotExpectedError {
			return fmt.Errorf("didn't get one of expected container errors: %s", strings.Join(expectedContainerErrors, " | "))
		}
		return nil
	}, timeout, pollPeriond, consistencyPeriod)
}

// Wait waits for pod to start and checks that it doesn't fail immediately after that
func (pi *PodInterface) Wait(timing ...time.Duration) error {
	return pi.WaitForPodStatus(nil, timing...)
}

// WaitForDestruction waits for the pod to be deleted
func (pi *PodInterface) WaitForDestruction(timing ...time.Duration) error {
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
		if _, err := pi.controller.client.Pods(pi.Pod.Namespace).Get(pi.Pod.Name, metav1.GetOptions{}); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return fmt.Errorf("pod %s was not deleted", pi.Pod.Name)
	}, timeout, pollPeriond, consistencyPeriod)
}

// Container returns an interface to handle one of the pod's
// containers. If name is empty, it takes the first container
// of the pod.
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
		transport, upgrader, err := spdy.RoundTripperFor(pi.controller.restConfig)
		if err != nil {
			errCh <- err
			return
		}
		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
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

// LoadEvents retrieves the evnets for this pod as a list
// of strings of the form Type:Reason:Message
func (pi *PodInterface) LoadEvents() ([]string, error) {
	events, err := pi.controller.client.Events(pi.controller.Namespace()).Search(scheme.Scheme, pi.Pod)
	if err != nil {
		return nil, err
	}
	var r []string
	for _, e := range events.Items {
		r = append(r, fmt.Sprintf("%s:%s:%s", e.Type, e.Reason, e.Message))
	}
	return r, nil
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
	req.VersionedParams(&v1.PodExecOptions{
		Container: ci.name,
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(ci.podInterface.controller.restConfig, "POST", req.URL())
	if err != nil {
		return err
	}

	options := remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
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

// Logs returns the logs of the container as a string.
func (ci *containerInterface) Logs() (string, error) {
	restClient := ci.podInterface.controller.client.RESTClient()
	req := restClient.Get().
		Name(ci.podInterface.Pod.Name).
		Namespace(ci.podInterface.Pod.Namespace).
		Resource("pods").
		SubResource("log")
	req.VersionedParams(&v1.PodLogOptions{
		Container: ci.name,
	}, scheme.ParameterCodec)
	stream, err := req.Stream()
	if err != nil {
		return "", err
	}
	defer stream.Close()

	bs, err := ioutil.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("ReadAll(): %v", err)
	}

	return string(bs), nil
}
