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

package config

import (
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	virtlet_v1 "github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
)

func configMappingProps() *apiext.JSONSchemaProps {
	return &apiext.JSONSchemaProps{
		Properties: map[string]apiext.JSONSchemaProps{
			"spec": {
				Properties: map[string]apiext.JSONSchemaProps{
					"nodeName": {
						Type: "string",
					},
					"nodeSelector": {
						Type: "object",
						// FIXME: https://github.com/kubernetes/kubernetes/issues/59485
						// AdditionalProperties: &apiext.JSONSchemaPropsOrBool{
						// 	Allows: true,
						// 	Schema: &apiext.JSONSchemaProps{
						// 		Type: "string",
						// 	},
						// },
					},
					"priority": {
						Type: "integer",
					},
					"config": {
						Properties: configFieldSet(&virtlet_v1.VirtletConfig{}).schemaProps(),
					},
				},
			},
		},
	}
}

// GetCRDDefinitions returns custom resource definitions for VirtletImageMapping kind in k8s.
func GetCRDDefinitions() []runtime.Object {
	gv := virtlet_v1.SchemeGroupVersion
	return []runtime.Object{
		&apiext.CustomResourceDefinition{
			TypeMeta: meta_v1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1beta1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: meta_v1.ObjectMeta{
				Labels: map[string]string{
					"virtlet.cloud": "",
				},
				Name: "virtletimagemappings." + gv.Group,
			},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group:   gv.Group,
				Version: gv.Version,
				Scope:   apiext.NamespaceScoped,
				Names: apiext.CustomResourceDefinitionNames{
					Plural:     "virtletimagemappings",
					Singular:   "virtletimagemapping",
					Kind:       "VirtletImageMapping",
					ShortNames: []string{"vim"},
				},
			},
		},
		&apiext.CustomResourceDefinition{
			TypeMeta: meta_v1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1beta1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: meta_v1.ObjectMeta{
				Labels: map[string]string{
					"virtlet.cloud": "",
				},
				Name: "virtletconfigmappings." + gv.Group,
			},
			Spec: apiext.CustomResourceDefinitionSpec{
				Group:   gv.Group,
				Version: gv.Version,
				Scope:   apiext.NamespaceScoped,
				Names: apiext.CustomResourceDefinitionNames{
					Plural:     "virtletconfigmappings",
					Singular:   "virtletconfigmapping",
					Kind:       "VirtletConfigMapping",
					ShortNames: []string{"vcm"},
				},
				Validation: &apiext.CustomResourceValidation{
					OpenAPIV3Schema: configMappingProps(),
				},
			},
		},
	}
}
