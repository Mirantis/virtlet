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

	"github.com/Mirantis/virtlet/pkg/api/types/v1"
	"github.com/Mirantis/virtlet/pkg/utils"
)

type crdConfigSource struct {
	namespace string
}

var _ ConfigSource = crdConfigSource{}

// Configs implements ConfigSource Configs
func (cs crdConfigSource) Configs(ctx context.Context) ([]TranslationConfig, error) {
	cfg, err := utils.GetK8sClientConfig("")
	if err != nil {
		return nil, err
	}

	if cfg.Host == "" {
		return nil, nil
	}

	client, err := v1.GetCRDRestClient(cfg)
	if err != nil {
		return nil, err
	}
	var list v1.VirtletImageMappingList
	err = client.Get().
		Context(ctx).
		Resource("virtletimagemappings").
		Namespace(cs.namespace).
		Do().Into(&list)
	if err != nil {
		return nil, err
	}
	result := make([]TranslationConfig, len(list.Items))
	for i, v := range list.Items {
		result[i] = &v
	}

	return result, nil
}

// Description implements ConfigSource Description
func (cs crdConfigSource) Description() string {
	return fmt.Sprintf("Kubernetes VirtletImageMapping resources in namespace %q", cs.namespace)
}

// NewCRDSource is a factory for CRD-based config source
func NewCRDSource(namespace string) ConfigSource {
	return crdConfigSource{namespace: namespace}
}
