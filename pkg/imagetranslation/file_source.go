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
	"fmt"
	"io/ioutil"
	"path"

	yaml "gopkg.in/yaml.v2"
)

type fileConfigSource struct {
	configsDirectory string
}

var _ ConfigSource = fileConfigSource{}

type fileConfig struct {
	name string
	cs   *fileConfigSource
}

var _ TranslationConfig = fileConfig{}

// Config implements ConfigSource Config
func (cs fileConfigSource) Configs(ctx context.Context) ([]TranslationConfig, error) {
	var result []TranslationConfig
	r, err := ioutil.ReadDir(cs.configsDirectory)
	if err != nil {
		return nil, err
	}
	for _, f := range r {
		if f.IsDir() {
			continue
		}
		result = append(result, fileConfig{name: f.Name(), cs: &cs})
	}
	return result, nil
}

// Description implements ConfigSource Description
func (cs fileConfigSource) Description() string {
	return fmt.Sprintf("local directory %s", cs.configsDirectory)
}

// Name implements TranslationConfig Name
func (c fileConfig) Name() string {
	return c.name
}

// Payload implements TranslationConfig Payload
func (c fileConfig) Payload() (ImageTranslation, error) {
	data, err := ioutil.ReadFile(path.Join(c.cs.configsDirectory, c.name))
	var tr ImageTranslation
	if err != nil {
		return tr, err
	}
	err = yaml.Unmarshal(data, &tr)
	return tr, err
}

// NewFileConfigSource is a factory for a directory-based config source
func NewFileConfigSource(configsDirectory string) ConfigSource {
	return fileConfigSource{configsDirectory: configsDirectory}
}
