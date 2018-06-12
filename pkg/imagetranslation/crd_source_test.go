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

package imagetranslation

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	virtlet_v1 "github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
	"github.com/Mirantis/virtlet/pkg/client/clientset/versioned/fake"
)

func TestCRDConfigSource(t *testing.T) {
	srcConfigs := []*virtlet_v1.VirtletImageMapping{
		{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "mapping1",
				Namespace: "foobar",
			},
			Spec: virtlet_v1.ImageTranslation{
				Rules: []virtlet_v1.TranslationRule{
					{
						Name: "testimage1",
						URL:  "https://example.com/testimage1.qcow2",
					},
				},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "mapping2",
				Namespace: "foobar",
			},
			Spec: virtlet_v1.ImageTranslation{
				Rules: []virtlet_v1.TranslationRule{
					{
						Name: "testimage2",
						URL:  "https://example.com/testimage2.qcow2",
					},
				},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "mapping3_skipme",
				Namespace: "skipthisns",
			},
			Spec: virtlet_v1.ImageTranslation{
				Rules: []virtlet_v1.TranslationRule{
					{
						Name: "testimage3_skipme",
						URL:  "https://example.com/nosuchimage.qcow2",
					},
				},
			},
		},
	}
	clientset := fake.NewSimpleClientset(srcConfigs[0], srcConfigs[1], srcConfigs[2])
	cs := NewCRDSource("foobar", nil)
	cs.(*crdConfigSource).virtletClient = clientset

	desc := cs.Description()
	if !strings.Contains(desc, "foobar") {
		t.Errorf("Namespace name 'foobar' not in CRD source description: %q", desc)
	}

	expectedConfigs := []TranslationConfig{srcConfigs[0], srcConfigs[1]}
	configs, err := cs.Configs(context.Background())
	if err != nil {
		t.Fatalf("Configs(): %v", err)
	}
	if !reflect.DeepEqual(expectedConfigs, configs) {
		expected, err := yaml.Marshal(expectedConfigs)
		if err != nil {
			t.Errorf("Error marshalling yaml: %v", err)
		}
		actual, err := yaml.Marshal(configs)
		if err != nil {
			t.Errorf("Error marshalling yaml: %v", err)
		}
		t.Errorf("Bad config list.\n--- expected configs ---\n%s\n--- actual configs ---\n%s", expected, actual)
	}
}
