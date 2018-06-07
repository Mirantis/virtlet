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
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// GetCRDDefinitions returns custom resource definitions for VirtletImageMapping kind in k8s.
func GetCRDDefinitions() []runtime.Object {
	return []runtime.Object{
		&apiextensionsv1beta1.CustomResourceDefinition{
			TypeMeta: meta_v1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1beta1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: meta_v1.ObjectMeta{
				Labels: map[string]string{
					"virtlet.cloud": "",
				},
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
		},
		&apiextensionsv1beta1.CustomResourceDefinition{
			TypeMeta: meta_v1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1beta1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: meta_v1.ObjectMeta{
				Labels: map[string]string{
					"virtlet.cloud": "",
				},
				Name: "virtletconfigmappings." + groupName,
			},
			Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
				Group:   groupName,
				Version: version,
				Scope:   apiextensionsv1beta1.NamespaceScoped,
				Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
					Plural:     "virtletconfigmappings",
					Singular:   "virtletconfigmapping",
					Kind:       "VirtletConfigMapping",
					ShortNames: []string{"vcm"},
				},
			},
		},
	}
}
