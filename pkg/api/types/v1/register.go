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

package v1

import (
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	groupName = "virtlet.k8s"
	version   = "v1"
)

var (
	schemeBuilder      = runtime.NewSchemeBuilder(addKnownTypes)
	scheme             = runtime.NewScheme()
	schemeGroupVersion = schema.GroupVersion{Group: groupName, Version: version}
)

// RegisterCustomResourceTypes registers custom resource definition for VirtletImageMapping kind in k8s
func RegisterCustomResourceTypes() error {
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
	cfg, err := utils.GetK8sClientConfig("")
	if err != nil || cfg.Host == "" {
		return err
	}
	extensionsClientSet, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	_, err = extensionsClientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(&crd)
	if err == nil || apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
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
