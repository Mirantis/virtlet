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

type Controller struct {
	fixedNs bool

	Client     *typedv1.CoreV1Client
	Namespace  *v1.Namespace
	RestConfig *restclient.Config
}

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
		Client:     clientset,
		Namespace:  ns,
		RestConfig: config,
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

func (c *Controller) Close() error {
	if c.fixedNs {
		return nil
	}
	return c.Client.Namespaces().Delete(c.Namespace.Name, nil)
}

func (c *Controller) VM(name string) *VMInterface {
	return newVMInterface(c, name)
}

func (c *Controller) Pod(name, namespace string) (*PodInterface, error) {
	if namespace == "" {
		namespace = c.Namespace.Name
	}
	pod, err := c.Client.Pods(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return newPodInterface(c, pod), nil
}

func (c *Controller) FindPod(namespace string, labelMap map[string]string,
	predicate func(podInterface *PodInterface) bool) (*PodInterface, error) {

	if namespace == "" {
		namespace = c.Namespace.Name
	}
	lst, err := c.Client.Pods(namespace).List(metav1.ListOptions{LabelSelector: labels.SelectorFromSet(labelMap).String()})
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

func (c *Controller) Node(name string, privileged bool, user string) (*NodeInterface, error) {
	return newNodeInterface(name, privileged, user)
}

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
		_, err := c.Client.Services(c.Namespace.Name).Create(svc)
		if err != nil {
			return nil, err
		}
	}
	return podInterface, nil
}
