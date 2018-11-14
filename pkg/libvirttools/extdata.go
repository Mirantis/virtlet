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

package libvirttools

import (
	"encoding/base64"
	"fmt"
	"strings"

	// use this instead of "gopkg.in/yaml.v2" so we don't get
	// map[interface{}]interface{} when unmarshalling cloud-init data
	"github.com/ghodss/yaml"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/utils"
)

func init() {
	types.SetExternalDataLoaders(loadExternalUserData, loadDSAsFileMap)
}

func loadExternalUserData(va *types.VirtletAnnotations, ns string, podAnnotations map[string]string) error {
	if ns == "" {
		return nil
	}
	var clientset *kubernetes.Clientset
	var err error
	userDataSourceKey := podAnnotations[types.CloudInitUserDataSourceKeyName]
	sshKeySourceKey := podAnnotations[types.SSHKeySourceKeyName]
	if userDataSourceKey != "" || sshKeySourceKey != "" {
		clientset, err = utils.GetK8sClientset(nil)
		if err != nil {
			return err
		}
	}
	if userDataSourceKey != "" {
		err = loadUserDataFromDataSource(va, ns, userDataSourceKey, clientset)
		if err != nil {
			return err
		}
	}
	if sshKeySourceKey != "" {
		err = loadSSHKeysFromDataSource(va, ns, sshKeySourceKey, clientset)
		if err != nil {
			return err
		}
	}
	return nil
}

func loadUserDataFromDataSource(va *types.VirtletAnnotations, ns, key string, clientset *kubernetes.Clientset) error {
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid %s annotation format. Expected kind/name, but insted got %s", types.CloudInitUserDataSourceKeyName, key)
	}
	ud, err := readK8sKeySource(parts[0], parts[1], ns, "", clientset)
	if err != nil {
		return err
	}
	va.UserData = map[string]interface{}{}
	for k, v := range ud {
		var value interface{}
		if yaml.Unmarshal([]byte(v), &value) == nil {
			va.UserData[k] = value
		}
	}
	return nil
}

func loadSSHKeysFromDataSource(va *types.VirtletAnnotations, ns, key string, clientset *kubernetes.Clientset) error {
	parts := strings.Split(key, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return fmt.Errorf("invalid %s annotation format. Expected kind/name[/key], but insted got %s", types.SSHKeySourceKeyName, key)
	}
	dataKey := "authorized_keys"
	if len(parts) == 3 {
		dataKey = parts[2]
	}
	ud, err := readK8sKeySource(parts[0], parts[1], ns, dataKey, clientset)
	if err != nil {
		return err
	}
	sshKeys := ud[dataKey]
	keys := strings.Split(sshKeys, "\n")
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			va.SSHKeys = append(va.SSHKeys, k)
		}
	}
	return nil
}

func readK8sKeySource(sourceType, sourceName, ns, key string, clientset *kubernetes.Clientset) (map[string]string, error) {
	sourceType = strings.ToLower(sourceType)
	switch sourceType {
	case "secret":
		secret, err := clientset.CoreV1().Secrets(ns).Get(sourceName, meta_v1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if key != "" {
			return map[string]string{key: string(secret.Data[key])}, nil
		}
		result := map[string]string{}
		for k, v := range secret.Data {
			result[k] = string(v)
		}
		return result, nil
	case "configmap":
		configmap, err := clientset.CoreV1().ConfigMaps(ns).Get(sourceName, meta_v1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if key != "" {
			return map[string]string{key: configmap.Data[key]}, nil
		}
		result := map[string]string{}
		for k, v := range configmap.Data {
			result[k] = v
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported source kind %s. Must be one of (secret, configmap)", sourceType)
	}
}

func loadDSAsFileMap(ns, key string) (map[string][]byte, error) {
	if ns == "" {
		return nil, nil
	}
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid %s annotation format. Expected kind/name, but insted got %s", types.FilesFromDSKeyName, key)
	}
	clientset, err := utils.GetK8sClientset(nil)
	if err != nil {
		return nil, fmt.Errorf("cannot read files from data source %q: %v", key, err)
	}

	data, err := readK8sKeySource(parts[0], parts[1], ns, "", clientset)
	if err != nil {
		return nil, err
	}

	return parseDataAsFileMap(data)
}

func parseDataAsFileMap(data map[string]string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	for k, v := range data {
		if strings.HasSuffix(k, "_path") || strings.HasSuffix(k, "_encoding") {
			continue
		}

		path, pOk := data[k+"_path"]
		if !pOk {
			return nil, fmt.Errorf("missing path for %q entry", k)
		}

		encoding, eOk := data[k+"_encoding"]
		if !eOk {
			encoding = "base64"
		}

		switch encoding {
		case "plain":
			files[path] = []byte(v)
		case "base64":
			data, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return nil, fmt.Errorf("cannot decode data under %q key as base64 encoded: %v", k, err)
			}
			files[path] = data
		default:
			return nil, fmt.Errorf("unkonwn encoding %q for %q", encoding, k)
		}
	}
	return files, nil
}

// TODO: create a test for loadExternalUserData
