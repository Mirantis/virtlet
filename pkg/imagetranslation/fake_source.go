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
	"context"

	"github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
)

type objectConfig struct {
	name        string
	translation v1.ImageTranslation
}

var _ TranslationConfig = objectConfig{}

// Name implements TranslationConfig Name
func (c objectConfig) ConfigName() string {
	return c.name
}

// Payload implements TranslationConfig Payload
func (c objectConfig) Payload() (v1.ImageTranslation, error) {
	return c.translation, nil
}

type fakeConfigSource struct {
	configs map[string]v1.ImageTranslation
}

var _ ConfigSource = fakeConfigSource{}

// Configs implements ConfigSource Configs
func (cs fakeConfigSource) Configs(ctx context.Context) ([]TranslationConfig, error) {
	var result []TranslationConfig
	for name, tr := range cs.configs {
		result = append(result, objectConfig{name: name, translation: tr})
	}
	return result, nil
}

// Description implements ConfigSource Description
func (cs fakeConfigSource) Description() string {
	return "fake config source"
}

// NewFakeConfigSource is a factory for a fake config source
func NewFakeConfigSource(configs map[string]v1.ImageTranslation) ConfigSource {
	return &fakeConfigSource{configs: configs}
}
