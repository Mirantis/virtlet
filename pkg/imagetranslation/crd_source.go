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

package imagetranslation

import (
	"fmt"
	"os"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

const groupName = "virtlet.k8s"
const version = "v1"

var (
	schemeBuilder      = runtime.NewSchemeBuilder(addKnownTypes)
	scheme             = runtime.NewScheme()
	schemeGroupVersion = schema.GroupVersion{Group: groupName, Version: version}
)

// VirtletImageMapping represents an ImageTranslation wrapped in k8s object
type VirtletImageMapping struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`
	Spec               ImageTranslation `json:"spec"`
}

var _ TranslationConfig = VirtletImageMapping{}

// VirtletImageMappingList is a k8s representation of list of translation configs
type VirtletImageMappingList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []VirtletImageMapping `json:"items"`
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(schemeGroupVersion,
		&VirtletImageMapping{},
		&VirtletImageMappingList{},
	)
	meta_v1.AddToGroupVersion(scheme, schemeGroupVersion)
	return nil
}

func init() {
	if err := schemeBuilder.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

func getClientConfig() (*rest.Config, error) {
	url, exists := os.LookupEnv("KUBERNETES_CLUSTER_URL")
	if !exists {
		return rest.InClusterConfig()
	}
	return &rest.Config{Host: url}, nil
}

// RegisterCustomResourceType registers custom resource definition for VirtletImageMapping kind in k8s
func RegisterCustomResourceType() error {
	crd := apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "virtletimagemappings." + groupName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   groupName,
			Version: version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     "virtletimagemappings",
				Singular:   "virtletimagemapping",
				Kind:       "VirtletImageMapping",
				ShortNames: []string{"vim"},
			},
		},
	}
	cfg, err := getClientConfig()
	if err != nil || cfg.Host == "" {
		return err
	}
	extensionsClientSet, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	_, err = extensionsClientSet.CustomResourceDefinitions().Create(&crd)

	if err == nil || errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// Name implements TranslationConfig Name
func (vim VirtletImageMapping) Name() string {
	return vim.ObjectMeta.Name
}

// Payload implements TranslationConfig Payload
func (vim VirtletImageMapping) Payload() (ImageTranslation, error) {
	return vim.Spec, nil
}

type crdConfigSource struct {
	namespace string
}

var _ ConfigSource = crdConfigSource{}

// Configs implements ConfigSource Configs
func (cs crdConfigSource) Configs() ([]TranslationConfig, error) {
	cfg, err := getClientConfig()
	if err != nil {
		return nil, err
	}

	if cfg.Host == "" {
		return nil, nil
	}

	client, err := GetCRDRestClient(cfg)
	if err != nil {
		return nil, err
	}
	var list VirtletImageMappingList
	err = client.Get().
		Resource("virtletimagemappings").
		Namespace(cs.namespace).
		Do().Into(&list)
	if err != nil {
		return nil, err
	}
	result := make([]TranslationConfig, len(list.Items))
	for i, v := range list.Items {
		result[i] = v
	}

	return result, nil
}

// GetCRDRestClient returns ReST client that can be used to work with virtlet CRDs
func GetCRDRestClient(cfg *rest.Config) (*rest.RESTClient, error) {
	config := *cfg
	config.GroupVersion = &schemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// Description implements ConfigSource Description
func (cs crdConfigSource) Description() string {
	return fmt.Sprintf("Kubernetes VirtletImageMapping resources in namespace %q", cs.namespace)
}

// NewCRDSource is a factory for CRD-based config source
func NewCRDSource(namespace string) ConfigSource {
	return crdConfigSource{namespace: namespace}
}
