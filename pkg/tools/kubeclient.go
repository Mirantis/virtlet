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
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	v1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
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

// ForwardedPort specifies an entry for the PortForward request
type ForwardedPort struct {
	// LocalPort specifies the local port to use. 0 means selecting
	// a random local port.
	LocalPort uint16
	// RemotePort specifies the remote (pod-side) port to use.
	RemotePort uint16
}

func (fp ForwardedPort) String() string {
	if fp.LocalPort == 0 {
		return fmt.Sprintf(":%d", fp.RemotePort)
	}
	return fmt.Sprintf("%d:%d", fp.LocalPort, fp.RemotePort)
}

// NOTE: this regexp ignores ipv6 port forward lines
var portForwardRx = regexp.MustCompile(`Forwarding from [^[]*:(\d+) -> \d+`)

func parsePortForwardOutput(out string, ports []*ForwardedPort) error {
	var localPorts []uint16
	for _, l := range strings.Split(out, "\n") {
		m := portForwardRx.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		port, err := strconv.ParseUint(m[1], 10, 16)
		if err != nil {
			return fmt.Errorf("bad port forward line (can't parse the local port): %q", l)
		}
		localPorts = append(localPorts, uint16(port))
	}
	if len(localPorts) != len(ports) {
		return fmt.Errorf("bad port forward output (expected %d ports, got %d). Full output from the forwarder:\n%s", len(ports), len(localPorts), out)
	}
	for n, lp := range localPorts {
		switch {
		case ports[n].LocalPort == 0:
			ports[n].LocalPort = lp
			continue
		case ports[n].LocalPort != lp:
			return fmt.Errorf("port mismatch: %d instead of %d for the remote port %d. Full output from the forwarder:\n%s", lp, ports[n].LocalPort, ports[n].RemotePort, out)
		}
	}
	return nil
}

// KubeClient contains methods for interfacing with Kubernetes clusters.
type KubeClient interface {
	// GetVirtletPodNames returns a list of names of the virtlet pods
	// present in the cluster.
	GetVirtletPodNames() ([]string, error)
	// GetVirtletPodNameForNode returns a name of the virtlet pod on
	// the specified k8s node.
	GetVirtletPodNameForNode(nodeName string) (string, error)
	// GetVMPodInfo returns then name of the virtlet pod and the vm container name for
	// the specified VM pod.
	GetVMPodInfo(podName string) (*VMPodInfo, error)
	// ExecInContainer given a pod, a container, a namespace and a command
	// executes that command inside the pod's container returning stdout and stderr output
	// as strings and an error if it has occurred.
	// The specified stdin, stdout and stderr are used as the
	// standard input / output / error streams of the remote command.
	// No TTY is allocated by this function stdin.
	ExecInContainer(podName, containerName, namespace string, stdin io.Reader, stdout, stderr io.Writer, command []string) (int, error)
	// ForwardPorts starts forwarding the specified ports to the specified pod in background.
	// If a port entry has LocalPort = 0, it's updated with the real port number that was
	// selected by the forwarder.
	// The function returns when the ports are ready for use or if/when an error occurs.
	// Close stopCh to stop the port forwarder.
	ForwardPorts(podName, namespace string, ports []*ForwardedPort) (stopCh chan struct{}, err error)
}

type remoteExecutor interface {
	Stream(config *rest.Config, method string, url *url.URL, options remotecommand.StreamOptions) error
}

type defaultExecutor struct{}

var _ remoteExecutor = defaultExecutor{}

func (e defaultExecutor) Stream(config *rest.Config, method string, url *url.URL, options remotecommand.StreamOptions) error {
	executor, err := remotecommand.NewExecutor(config, method, url)
	if err != nil {
		return err
	}
	return executor.Stream(options)
}

type portForwarder interface {
	ForwardPorts(config *rest.Config, method string, url *url.URL, ports []string, stopChannel, readyChannel chan struct{}, out io.Writer) error
}

type defaultPortForwarder struct{}

var _ portForwarder = defaultPortForwarder{}

func (pf defaultPortForwarder) ForwardPorts(config *rest.Config, method string, url *url.URL, ports []string, stopChannel, readyChannel chan struct{}, out io.Writer) error {
	dialer, err := remotecommand.NewExecutor(config, method, url)
	if err != nil {
		return err
	}
	fw, err := portforward.New(dialer, ports, stopChannel, readyChannel, out, os.Stderr)
	if err != nil {
		return err
	}
	return fw.ForwardPorts()
}

// RealKubeClient is used to access a Kubernetes cluster.
type RealKubeClient struct {
	client        kubernetes.Interface
	clientCfg     clientcmd.ClientConfig
	restClient    rest.Interface
	config        *rest.Config
	namespace     string
	executor      remoteExecutor
	portForwarder portForwarder
}

var _ KubeClient = &RealKubeClient{}

// NewRealKubeClient creates a RealKubeClient for the specified ClientConfig.
func NewRealKubeClient(clientCfg clientcmd.ClientConfig) *RealKubeClient {
	return &RealKubeClient{
		clientCfg:     clientCfg,
		executor:      defaultExecutor{},
		portForwarder: defaultPortForwarder{},
	}
}

func (c *RealKubeClient) setup() error {
	if c.client != nil {
		return nil
	}

	config, err := c.clientCfg.ClientConfig()
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("Can't create kubernetes api client: %v", err)
	}

	ns, _, err := c.clientCfg.Namespace()
	if err != nil {
		return err
	}

	c.client = client
	c.config = config
	c.namespace = ns
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

// GetVirtletPodNames implements GetVirtletPodNames method of KubeClient interface.
func (c *RealKubeClient) GetVirtletPodNames() ([]string, error) {
	return c.getVirtletPodNames("")
}

// GetVirtletPodNameForNode implements GetVirtletPodNameForNode method of KubeClient interface.
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

// GetVMPodInfo implements GetVMPodInfo method of KubeClient interface.
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

// ExecInContainer implements ExecInContainer method of KubeClient interface
func (c *RealKubeClient) ExecInContainer(podName, containerName, namespace string, stdin io.Reader, stdout, stderr io.Writer, command []string) (int, error) {
	if err := c.setup(); err != nil {
		return 0, err
	}
	if namespace == "" {
		namespace = c.namespace
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

	exitCode := 0
	if err := c.executor.Stream(c.config, "POST", req.URL(), remotecommand.StreamOptions{
		SupportedProtocols: remotecommandconsts.SupportedStreamingProtocols,
		Stdin:              stdin,
		Stdout:             stdout,
		Stderr:             stderr,
	}); err != nil {
		if c, ok := err.(exec.CodeExitError); ok {
			exitCode = c.Code
			err = nil
		}
	}

	return exitCode, nil
}

// ForwardPorts implements ForwardPorts method of KubeClient interface.
func (c *RealKubeClient) ForwardPorts(podName, namespace string, ports []*ForwardedPort) (stopCh chan struct{}, err error) {
	if len(ports) == 0 {
		return nil, errors.New("no ports specified")
	}

	if err := c.setup(); err != nil {
		return nil, err
	}

	if namespace == "" {
		namespace = c.namespace
	}

	pod, err := c.client.CoreV1().Pods(namespace).Get(podName, meta_v1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if pod.Status.Phase != v1.PodRunning {
		return nil, fmt.Errorf("unable to forward port because pod is not running (current status is %v)", pod.Status.Phase)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	stopCh = make(chan struct{})
	go func() {
		<-signals
		if stopCh != nil {
			close(stopCh)
		}
	}()

	req := c.restClient.Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod.Name).
		SubResource("portforward")
	var buf bytes.Buffer
	var portStrs []string
	for _, p := range ports {
		portStrs = append(portStrs, p.String())
	}
	errCh := make(chan error, 1)
	readyCh := make(chan struct{})
	go func() {
		errCh <- c.portForwarder.ForwardPorts(c.config, "POST", req.URL(), portStrs, stopCh, readyCh, &buf)
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-readyCh:
		// FIXME: there appears to be no better way to get back the local ports as of now
		if err := parsePortForwardOutput(buf.String(), ports); err != nil {
			return nil, err
		}
	}
	return stopCh, nil
}
