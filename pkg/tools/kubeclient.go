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
	"fmt"
	"io"
	"net/url"
	"strings"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	v1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"

	// register standard k8s types
	_ "k8s.io/client-go/pkg/api/install"
)

// VMPodInfo describes a VM pod in a way that's necessary for virtletctl to
// handle it
type VMPodInfo struct {
	// NodeName is the name of the node where the VM pod runs
	NodeName string
	// VirtletPodName is the name of the virtlet pod that manages this VM pod
	VirtletPodName string
	// ContainerId is the id of the container in the VM pod
	ContainerId string
	// ContainerName is the name of the container in the VM pod
	ContainerName string
}

// LibvirtDomainName returns the name of the libvirt domain for the VMPodInfo.
func (podInfo VMPodInfo) LibvirtDomainName() string {
	containerId := podInfo.ContainerId
	if p := strings.Index(containerId, "__"); p >= 0 {
		containerId = containerId[p+2:]
	}
	if len(containerId) > 13 {
		containerId = containerId[:13]
	}
	return fmt.Sprintf("virtlet-%s-%s", containerId, podInfo.ContainerName)
}

type KubeClient interface {
	GetVirtletPodNames() ([]string, error)
	GetVirtletPodNameForNode(nodeName string) (string, error)
	GetVMPodInfo(podName string) (*VMPodInfo, error)
	ExecInContainer(podName, containerName, namespace string,
		stdin io.Reader, stdout, stderr io.Writer,
		command []string) (int, error)
}

type ExecutorFactory func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error)

func defaultExecutorFactory(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
	return remotecommand.NewExecutor(config, method, url)
}

// RealKubeClient is used to access a Kubernetes cluster.
type RealKubeClient struct {
	executorFactory ExecutorFactory
	client          kubernetes.Interface
	restClient      rest.Interface
	config          *rest.Config
	namespace       string
}

var _ KubeClient = &RealKubeClient{}

func NewRealKubeClient() *RealKubeClient {
	return &RealKubeClient{}
}

func (c *RealKubeClient) setup() error {
	if c.executorFactory == nil {
		c.executorFactory = defaultExecutorFactory
	}
	if c.client != nil {
		return nil
	}
	config, namespace, client, err := getApiClient()
	if err != nil {
		return err
	}
	c.client = client
	c.config = config
	c.namespace = namespace
	c.restClient = client.CoreV1().RESTClient()
	return nil
}

func (c *RealKubeClient) getVirtletPodNames(nodeName string) ([]string, error) {
	if err := c.setup(); err != nil {
		return nil, err
	}
	opts := meta_v1.ListOptions{
		LabelSelector: "runtime=virtlet",
	}
	if nodeName != "" {
		opts.FieldSelector = "spec.nodeName=" + nodeName
	}
	pods, err := c.client.CoreV1().Pods("kube-system").List(opts)
	if err != nil {
		return nil, err
	}

	var r []string
	for _, item := range pods.Items {
		r = append(r, item.Name)
	}
	return r, nil
}

func (c *RealKubeClient) getVMPod(podName string) (*v1.Pod, error) {
	if err := c.setup(); err != nil {
		return nil, err
	}
	return c.client.CoreV1().Pods(c.namespace).Get(podName, meta_v1.GetOptions{})
}

// GetVirtletPodNames returns a list of names of the virtlet pods
// present in the cluster.
func (c *RealKubeClient) GetVirtletPodNames() ([]string, error) {
	return c.getVirtletPodNames("")
}

// GetVirtletPodNameForNode returns a name of the virtlet pod on
// the specified k8s node.
func (c *RealKubeClient) GetVirtletPodNameForNode(nodeName string) (string, error) {
	virtletPodNames, err := c.getVirtletPodNames(nodeName)
	if err != nil {
		return "", err
	}

	if len(virtletPodNames) == 0 {
		return "", fmt.Errorf("no Virtlet pods found on the node %q", nodeName)
	}

	if len(virtletPodNames) > 1 {
		return "", fmt.Errorf("more than one Virtlet pod found on the node %q", nodeName)
	}

	return virtletPodNames[0], nil
}

// GetVMPodInfo returns then name of the virtlet pod and the vm container name for
// the specified VM pod.
func (c *RealKubeClient) GetVMPodInfo(podName string) (*VMPodInfo, error) {
	pod, err := c.getVMPod(podName)
	if err != nil {
		return nil, err
	}
	if pod.Spec.NodeName == "" {
		return nil, fmt.Errorf("pod %q doesn't have a node associated with it", podName)
	}
	if len(pod.Spec.Containers) != 1 {
		return nil, fmt.Errorf("vm pod %q is expected to have just one container but it has %d containers instead", podName, len(pod.Spec.Containers))
	}

	if len(pod.Status.ContainerStatuses) != 1 {
		return nil, fmt.Errorf("vm pod %q is expected to have just one container status but it has %d container statuses instead", podName, len(pod.Status.ContainerStatuses))
	}

	virtletPodName, err := c.GetVirtletPodNameForNode(pod.Spec.NodeName)
	if err != nil {
		return nil, err
	}

	return &VMPodInfo{
		NodeName:       pod.Spec.NodeName,
		VirtletPodName: virtletPodName,
		ContainerId:    pod.Status.ContainerStatuses[0].ContainerID,
		ContainerName:  pod.Spec.Containers[0].Name,
	}, nil
}

// ExecCommandOnContainer given a pod, container, namespace and command
// executes that command remotely returning stdout and stderr output
// as strings and an error if it occured.
// The specified stdin, stdout and stderr are used as the
// standard input / output / error streams of the remote command.
// No TTY is allocated by this function stdin.
func (c *RealKubeClient) ExecInContainer(
	podName, containerName, namespace string,
	stdin io.Reader, stdout, stderr io.Writer,
	command []string,
) (int, error) {
	if err := c.setup(); err != nil {
		return 0, err
	}
	req := c.restClient.Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&api.PodExecOptions{
			Container: containerName,
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
