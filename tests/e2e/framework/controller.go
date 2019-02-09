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

// Some parts of this file are borrowed from Kubernetes source
// (test/utils/density_utils.go). The original copyright notice
// follows.

/*
Copyright 2016 The Kubernetes Authors.
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
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	virtlet_v1 "github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
	virtletclientv1 "github.com/Mirantis/virtlet/pkg/client/clientset/versioned/typed/virtlet.k8s/v1"
)

const (
	retries              = 5
	defaultRunPodTimeout = 4 * time.Minute
)

var ClusterURL = flag.String("cluster-url", "http://127.0.0.1:8080", "apiserver URL")

// HostPathMount specifies a host path to mount into a pod sandbox.
type HostPathMount struct {
	// The path on the host.
	HostPath string
	// The path inside the container.
	ContainerPath string
}

// RunPodOptions specifies the options for RunPod
type RunPodOptions struct {
	// The command to run (optional).
	Command []string
	// Timeout. Defaults to 4 minutes.
	Timeout time.Duration
	// The list of ports to expose.
	ExposePorts []int32
	// The list of host paths to mount.
	HostPathMounts []HostPathMount
	// Node name to run this pod on.
	NodeName string
}

// Controller is the entry point for various operations on k8s+virtlet entities
type Controller struct {
	fixedNs bool

	client        typedv1.CoreV1Interface
	virtletClient virtletclientv1.VirtletV1Interface
	namespace     *v1.Namespace
	restConfig    *restclient.Config
}

// NewController creates instance of controller for specified k8s namespace.
// If namespace is empty string then namespace with random name is going to be created
func NewController(namespace string) (*Controller, error) {
	config, err := clientcmd.BuildConfigFromFlags(*ClusterURL, "")
	if err != nil {
		return nil, err
	}

	clientset, err := typedv1.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	virtletClient, err := virtletclientv1.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	var ns *v1.Namespace
	if namespace != "" {
		ns, err = clientset.Namespaces().Get(namespace, metav1.GetOptions{})
	} else {
		ns, err = createNamespace(clientset)
	}
	if err != nil {
		return nil, err
	}

	return &Controller{
		client:        clientset,
		virtletClient: virtletClient,
		namespace:     ns,
		restConfig:    config,
		fixedNs:       namespace != "",
	}, nil
}

func createNamespace(client *typedv1.CoreV1Client) (*v1.Namespace, error) {
	namespaceObj := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "virtlet-tests-",
		},
		Status: v1.NamespaceStatus{},
	}
	return client.Namespaces().Create(namespaceObj)
}

// Finalize deletes random namespace that might has been created by NewController
func (c *Controller) Finalize() error {
	if c.fixedNs {
		return nil
	}
	return c.client.Namespaces().Delete(c.namespace.Name, nil)
}

func (c *Controller) CreateVirtletImageMapping(mapping virtlet_v1.VirtletImageMapping) (*virtlet_v1.VirtletImageMapping, error) {
	return c.virtletClient.VirtletImageMappings("kube-system").Create(&mapping)
}

func (c *Controller) DeleteVirtletImageMapping(name string) error {
	return c.virtletClient.VirtletImageMappings("kube-system").Delete(name, &metav1.DeleteOptions{})
}

func (c *Controller) CreateVirtletConfigMapping(configMapping virtlet_v1.VirtletConfigMapping) (*virtlet_v1.VirtletConfigMapping, error) {
	return c.virtletClient.VirtletConfigMappings("kube-system").Create(&configMapping)
}

func (c *Controller) DeleteVirtletConfigMapping(name string) error {
	return c.virtletClient.VirtletConfigMappings("kube-system").Delete(name, &metav1.DeleteOptions{})
}

// PersistentVolumesClient returns interface for PVs
func (c *Controller) PersistentVolumesClient() typedv1.PersistentVolumeInterface {
	return c.client.PersistentVolumes()
}

// PersistentVolumeClaimsClient returns interface for PVCs
func (c *Controller) PersistentVolumeClaimsClient() typedv1.PersistentVolumeClaimInterface {
	return c.client.PersistentVolumeClaims(c.namespace.Name)
}

// ConfigMaps returns interface for ConfigMap objects
func (c *Controller) ConfigMaps() typedv1.ConfigMapInterface {
	return c.client.ConfigMaps(c.namespace.Name)
}

// Secrets returns interface for Secret objects
func (c *Controller) Secrets() typedv1.SecretInterface {
	return c.client.Secrets(c.namespace.Name)
}

// VM returns interface for operations on virtlet VM pods
func (c *Controller) VM(name string) *VMInterface {
	return newVMInterface(c, name)
}

// Pod returns interface for operations on k8s pod in a given namespace.
// If namespace is an empty string then default controller namespace is used
func (c *Controller) Pod(name, namespace string) (*PodInterface, error) {
	if namespace == "" {
		namespace = c.namespace.Name
	}
	pod, err := c.client.Pods(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return newPodInterface(c, pod), nil
}

// FindPod looks for a pod in a given namespace having specified labels and matching optional predicate function
func (c *Controller) FindPod(namespace string, labelMap map[string]string,
	predicate func(podInterface *PodInterface) bool) (*PodInterface, error) {

	if namespace == "" {
		namespace = c.namespace.Name
	}
	lst, err := c.client.Pods(namespace).List(metav1.ListOptions{LabelSelector: labels.SelectorFromSet(labelMap).String()})
	if err != nil {
		return nil, err
	}
	for _, pod := range lst.Items {
		pi := newPodInterface(c, &pod)
		if predicate == nil || predicate(pi) {
			return pi, nil
		}
	}
	return nil, nil
}

// VirtletPod returns one of the active virtlet pods
func (c *Controller) VirtletPod() (*PodInterface, error) {
	pod, err := c.FindPod("kube-system", map[string]string{"runtime": "virtlet"}, nil)
	if err != nil {
		return nil, err
	} else if pod == nil {
		return nil, fmt.Errorf("cannot find virtlet pod")
	}
	return pod, nil
}

// VirtletNodeName returns the name of one of the nodes that run Virtlet
func (c *Controller) VirtletNodeName() (string, error) {
	virtletPod, err := c.VirtletPod()
	if err != nil {
		return "", err
	}
	return virtletPod.Pod.Spec.NodeName, nil
}

// AvailableNodeName returns the name of a node that doesn't run
// Virtlet after the standard test setup is done but which can be
// labelled to run Virtlet.
func (c *Controller) AvailableNodeName() (string, error) {
	virtletNodeName, err := c.VirtletNodeName()
	if err != nil {
		return "", err
	}

	nodeList, err := c.client.Nodes().List(metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, node := range nodeList.Items {
		if node.Name == virtletNodeName {
			continue
		}
		if len(node.Spec.Taints) == 0 {
			return node.Name, nil
		}
	}

	return "", errors.New("couldn't find an available node")
}

// AddLabelsToNode adds the specified labels to the node.
// Based on test/utils/density_utils.go in the Kubernetes source.
func (c *Controller) AddLabelsToNode(nodeName string, labels map[string]string) error {
	tokens := make([]string, 0, len(labels))
	for k, v := range labels {
		tokens = append(tokens, "\""+k+"\":\""+v+"\"")
	}
	labelString := "{" + strings.Join(tokens, ",") + "}"
	patch := fmt.Sprintf(`{"metadata":{"labels":%v}}`, labelString)
	var err error
	for attempt := 0; attempt < retries; attempt++ {
		_, err = c.client.Nodes().Patch(nodeName, types.MergePatchType, []byte(patch))
		if err != nil {
			if !apierrs.IsConflict(err) {
				return err
			}
		} else {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// RemoveLabelOffNode is for cleaning up labels temporarily added to node,
// won't fail if target label doesn't exist or has been removed.
// Based on test/utils/density_utils.go in the Kubernetes source.
func (c *Controller) RemoveLabelOffNode(nodeName string, labelKeys []string) error {
	var node *v1.Node
	var err error
	for attempt := 0; attempt < retries; attempt++ {
		node, err = c.client.Nodes().Get(nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if node.Labels == nil {
			return nil
		}
		for _, labelKey := range labelKeys {
			if node.Labels == nil || len(node.Labels[labelKey]) == 0 {
				break
			}
			delete(node.Labels, labelKey)
		}
		_, err = c.client.Nodes().Update(node)
		if err != nil {
			if !apierrs.IsConflict(err) {
				return err
			}
		} else {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

func (c *Controller) WaitForVirtletPodOnTheNode(name string) (*PodInterface, error) {
	var virtletPod *PodInterface
	if err := waitFor(func() error {
		var err error
		virtletPod, err = c.FindPod("kube-system", map[string]string{"runtime": "virtlet"}, func(podInterface *PodInterface) bool {
			return podInterface.Pod.Spec.NodeName == name
		})
		switch {
		case err != nil:
			return err
		case virtletPod != nil:
			return nil
		default:
			return fmt.Errorf("no Virtlet pod on the node %q", name)
		}
	}, 5*time.Minute, 5*time.Second, false); err != nil {
		return nil, err
	}

	if err := virtletPod.Wait(5 * time.Minute); err != nil {
		return nil, err
	}

	return virtletPod, nil
}

func (c *Controller) WaitForVirtletPodToDisappearFromTheNode(name string) error {
	return waitFor(func() error {
		virtletPod, err := c.FindPod("kube-system", map[string]string{"runtime": "virtlet"}, func(podInterface *PodInterface) bool {
			return podInterface.Pod.Spec.NodeName == name
		})
		switch {
		case err != nil:
			return err
		case virtletPod == nil:
			return nil
		default:
			return fmt.Errorf("Virtlet pod still present on the node %q", name)
		}
	}, 5*time.Minute, 5*time.Second, false)
}

// DinDNodeExecutor returns executor in DinD container for one of k8s nodes
func (c *Controller) DinDNodeExecutor(name string) (Executor, error) {
	dockerInterface, err := newDockerContainerInterface(name)
	if err != nil {
		return nil, err
	}
	return dockerInterface.Executor(false, ""), nil
}

// DockerContainer returns interface for operations on a docker container with a given name
func (c *Controller) DockerContainer(name string) (*DockerContainerInterface, error) {
	return newDockerContainerInterface(name)
}

// Namespace returns default controller namespace name
func (c *Controller) Namespace() string {
	return c.namespace.Name
}

// RunPod is a helper method to create a pod in a simple configuration (similar to `kubectl run`)
func (c *Controller) RunPod(name, image string, opts RunPodOptions) (*PodInterface, error) {
	if opts.Timeout == 0 {
		opts.Timeout = defaultRunPodTimeout
	}
	pod := generatePodSpec(name, image, opts)
	podInterface := newPodInterface(c, pod)
	if err := podInterface.Create(); err != nil {
		return nil, err
	}
	if err := podInterface.Wait(opts.Timeout); err != nil {
		return nil, err
	}
	if len(opts.ExposePorts) > 0 {
		svc := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: v1.ServiceSpec{
				Selector: map[string]string{"id": name},
			},
		}
		for _, port := range opts.ExposePorts {
			svc.Spec.Ports = append(svc.Spec.Ports, v1.ServicePort{
				Name: fmt.Sprintf("port%d", port),
				Port: port,
			})
		}
		_, err := c.client.Services(c.namespace.Name).Create(svc)
		if err != nil {
			return nil, err
		}
		podInterface.hasService = true
	}
	return podInterface, nil
}

func generatePodSpec(name, image string, opts RunPodOptions) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"id": name},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            name,
					Image:           image,
					ImagePullPolicy: v1.PullIfNotPresent,
					Command:         opts.Command,
				},
			},
		},
	}

	if opts.NodeName != "" {
		pod.Spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": opts.NodeName,
		}
	}

	for n, hpm := range opts.HostPathMounts {
		name := fmt.Sprintf("vol%d", n)
		pod.Spec.Volumes = append(pod.Spec.Volumes, v1.Volume{
			Name: name,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: hpm.HostPath,
				},
			},
		})
		pod.Spec.Containers[0].VolumeMounts = append(
			pod.Spec.Containers[0].VolumeMounts,
			v1.VolumeMount{
				Name:      name,
				MountPath: hpm.ContainerPath,
			})
	}

	return pod
}
