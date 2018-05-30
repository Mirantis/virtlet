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

	"github.com/Mirantis/virtlet/pkg/api/types/v1"
	"github.com/Mirantis/virtlet/pkg/image"
)

// TranslationConfig represents a single config (prefix + rule list) in a config-set
type TranslationConfig interface {
	// Name returns the config name (any string identifier)
	Name() string

	// Payload returns ImageTranslation object associated with the config
	Payload() (v1.ImageTranslation, error)
}

// ConfigSource is the data-source for translation configs
type ConfigSource interface {
	// Configs returns list of configs that are available in this data source
	Configs(ctx context.Context) ([]TranslationConfig, error)

	// Description returns the data-source description to be used in the logs
	Description() string
}

// ImageNameTranslator is the main translator interface
type ImageNameTranslator interface {
	// LoadConfigs initializes translator with configs from supplied data sources. All previous mappings are discarded.
	LoadConfigs(ctx context.Context, sources ...ConfigSource)

	// Translate translates image name to ins Endpoint. If no suitable mapping was found, the default Endpoint is returned
	Translate(name string) image.Endpoint
}
