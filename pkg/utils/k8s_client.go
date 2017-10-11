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

package utils

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// GetK8sClientConfig returns config that is needed to access k8s
func GetK8sClientConfig(host string) (*rest.Config, error) {
	if host == "" {
		url, exists := os.LookupEnv("KUBERNETES_CLUSTER_URL")
		if !exists {
			return rest.InClusterConfig()
		}
		host = url
	}
	return &rest.Config{Host: host}, nil
}

// GetK8sClientset returns clientset for standard k8s APIs
func GetK8sClientset(config *rest.Config) (*kubernetes.Clientset, error) {
	if config == nil {
		var err error
		config, err = GetK8sClientConfig("")
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}

// GetK8sRestClient returns k8s ReST client that for the giver API group-version/subset
func GetK8sRestClient(cfg *rest.Config, scheme *runtime.Scheme, groupVersion *schema.GroupVersion) (*rest.RESTClient, error) {
	config := *cfg
	config.GroupVersion = groupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return client, nil
}
