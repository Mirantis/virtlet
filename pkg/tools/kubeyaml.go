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
	"encoding/json"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

// LoadYaml loads a k8s YAML data and returns a slice of k8s objects
func LoadYaml(data []byte) ([]runtime.Object, error) {
	parts := bytes.Split(data, []byte("---"))
	var r []runtime.Object
	for _, part := range parts {
		part = bytes.TrimSpace(part)
		if len(part) == 0 {
			continue
		}
		obj, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(part), nil, nil)
		if err != nil {
			return nil, err
		}
		r = append(r, obj)
	}
	return r, nil
}

// ToYaml converts a slice of k8s objects to YAML
func ToYaml(objs []runtime.Object) ([]byte, error) {
	var out bytes.Buffer
	for _, obj := range objs {
		// the idea is from https://github.com/ant31/crd-validation/blob/master/pkg/cli-utils.go
		bs, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}

		var us unstructured.Unstructured
		if err := json.Unmarshal(bs, &us.Object); err != nil {
			return nil, err
		}

		unstructured.RemoveNestedField(us.Object, "status")

		bs, err = yaml.Marshal(us.Object)
		if err != nil {
			return nil, err
		}
		out.WriteString("---\n")
		out.Write(bs)
		out.WriteString("\n")
	}
	return out.Bytes(), nil
}
