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

	"k8s.io/client-go/tools/clientcmd"

	virtletclient "github.com/Mirantis/virtlet/pkg/client/clientset/versioned"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type crdConfigSource struct {
	clientCfg     clientcmd.ClientConfig
	virtletClient virtletclient.Interface
	namespace     string
}

var _ ConfigSource = &crdConfigSource{}

func (cs *crdConfigSource) setup() error {
	if cs.virtletClient != nil {
		return nil
	}

	config, err := cs.clientCfg.ClientConfig()
	if err != nil {
		return err
	}

	virtletClient, err := virtletclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("can't create Virtlet api client: %v", err)
	}
	cs.virtletClient = virtletClient
	return nil
}

// Configs implements ConfigSource Configs
func (cs *crdConfigSource) Configs(ctx context.Context) ([]TranslationConfig, error) {
	if err := cs.setup(); err != nil {
		return nil, err
	}

	list, err := cs.virtletClient.VirtletV1().VirtletImageMappings(cs.namespace).List(meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var r []TranslationConfig
	for n := range list.Items {
		r = append(r, &list.Items[n])
	}
	return r, nil
}

// Description implements ConfigSource Description
func (cs *crdConfigSource) Description() string {
	return fmt.Sprintf("Kubernetes VirtletImageMapping resources in namespace %q", cs.namespace)
}

// NewCRDSource is a factory for CRD-based config source
func NewCRDSource(namespace string, clientCfg clientcmd.ClientConfig) ConfigSource {
	return &crdConfigSource{namespace: namespace, clientCfg: clientCfg}
}
