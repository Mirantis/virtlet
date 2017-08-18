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
	"flag"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// register standard k28 types
	_ "k8s.io/kubernetes/pkg/api/install"
)

var url = flag.String("cluster-url", "http://127.0.0.1:8080", "apiserver URL")

// Controller is the entry point for various operations on k8s+virtlet entities
type Controller struct {
	fixedNs bool

	client     *typedv1.CoreV1Client
	namespace  *v1.Namespace
	restConfig *restclient.Config
}

// NewController creates instance of controller for specified k8s namespace.
// If namespace is empty string then namespace with random name is going to be created
func NewController(namespace string) (*Controller, error) {
	config, err := clientcmd.BuildConfigFromFlags(*url, "")
	if err != nil {
		return nil, err
	}

	clientset, err := typedv1.NewForConfig(config)
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
		client:     clientset,
		namespace:  ns,
		restConfig: config,
		fixedNs:    namespace != "",
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

// PersistentVolumesClient returns interface for PVs
func (c *Controller) PersistentVolumesClient() typedv1.PersistentVolumeInterface {
	return c.client.PersistentVolumes()
}

// PersistentVolumeClaimsClient returns interface for PVCs
func (c *Controller) PersistentVolumeClaimsClient() typedv1.PersistentVolumeClaimInterface {
	return c.client.PersistentVolumeClaims(c.namespace.Name)
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
func (c *Controller) RunPod(name, image string, command []string, timeout time.Duration, exposePorts ...int32) (*PodInterface, error) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"id": name},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    name,
					Image:   image,
					Command: command,
				},
			},
		},
	}
	podInterface := newPodInterface(c, pod)
	if err := podInterface.Create(); err != nil {
		return nil, err
	}
	if err := podInterface.Wait(timeout); err != nil {
		return nil, err
	}
	if len(exposePorts) > 0 {
		svc := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: v1.ServiceSpec{
				Selector: map[string]string{"id": name},
			},
		}
		for _, port := range exposePorts {
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
