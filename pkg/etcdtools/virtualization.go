/*
Copyright 2016 Mirantis

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

package etcdtools

import (
	"encoding/json"
	"fmt"
	"strings"

	etcd "github.com/coreos/etcd/client"
	"golang.org/x/net/context"
)

type VirtualizationTool struct {
	keysAPITool *KeysAPITool
}

func NewVirtualizationTool(keysAPITool *KeysAPITool) (*VirtualizationTool, error) {
	kapi, err := keysAPITool.newKeysAPI()
	if err != nil {
		return nil, err
	}
	if _, err := kapi.Set(context.Background(), "/container", "", &etcd.SetOptions{Dir: true}); err != nil {
		// 102 "Not a file error" means that the dir node already exists.
		// There is no way to tell etcd client to ignore this fact.
		// TODO(nhlfr): Report a bug in etcd about that.
		if !strings.Contains(err.Error(), "102") {
			return nil, err
		}
	}
	return &VirtualizationTool{keysAPITool: keysAPITool}, nil
}

func (v *VirtualizationTool) SetLabels(containerId string, labels map[string]string) error {
	kapi, err := v.keysAPITool.newKeysAPI()
	if err != nil {
		return err
	}

	strLabels, err := json.Marshal(labels)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("/container/%s/labels", containerId)

	if _, err := kapi.Set(context.Background(), key, string(strLabels), nil); err != nil {
		return err
	}

	return nil
}

func (v *VirtualizationTool) SetAnnotations(containerId string, annotations map[string]string) error {
	kapi, err := v.keysAPITool.newKeysAPI()
	if err != nil {
		return err
	}

	strAnnotations, err := json.Marshal(annotations)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("/container/%s/annotations", containerId)

	if _, err := kapi.Set(context.Background(), key, string(strAnnotations), nil); err != nil {
		return err
	}

	return nil
}

func (v *VirtualizationTool) GetLabels(containerId string) (map[string]string, error) {
	var labels map[string]string

	kapi, err := v.keysAPITool.newKeysAPI()
	if err != nil {
		return labels, err
	}

	key := fmt.Sprintf("/container/%s/labels", containerId)

	resp, err := kapi.Get(context.Background(), key, nil)
	if err != nil {
		return labels, err
	}

	if err := json.Unmarshal([]byte(resp.Node.Value), &labels); err != nil {
		return labels, err
	}

	return labels, nil
}

func (v *VirtualizationTool) GetAnnotations(containerId string) (map[string]string, error) {
	var annotations map[string]string

	kapi, err := v.keysAPITool.newKeysAPI()
	if err != nil {
		return annotations, err
	}

	key := fmt.Sprintf("/container/%s/annotations", containerId)

	resp, err := kapi.Get(context.Background(), key, nil)
	if err != nil {
		return annotations, err
	}

	if err := json.Unmarshal([]byte(resp.Node.Value), &annotations); err != nil {
		return annotations, err
	}

	return annotations, nil
}
