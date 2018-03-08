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
	"fmt"
	"strconv"
	"strings"

	// use this instead of "gopkg.in/yaml.v2" so we don't get
	// map[interface{}]interface{} when unmarshalling cloud-init data
	"github.com/ghodss/yaml"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Mirantis/virtlet/pkg/utils"
)

type diskDriverName string

type imageType string

const (
	maxVCPUCount                                     = 255
	vcpuCountAnnotationKeyName                       = "VirtletVCPUCount"
	cloudInitMetaDataKeyName                         = "VirtletCloudInitMetaData"
	cloudInitUserDataKeyName                         = "VirtletCloudInitUserData"
	cloudInitUserDataSourceKeyName                   = "VirtletCloudInitUserDataSource"
	cloudInitUserDataOverwriteKeyName                = "VirtletCloudInitUserDataOverwrite"
	cloudInitUserDataScriptKeyName                   = "VirtletCloudInitUserDataScript"
	cloudInitImageType                               = "VirtletCloudInitImageType"
	sshKeysKeyName                                   = "VirtletSSHKeys"
	sshKeySourceKeyName                              = "VirtletSSHKeySource"
	diskDriverKeyName                                = "VirtletDiskDriver"
	diskDriverVirtio                  diskDriverName = "virtio"
	diskDriverScsi                    diskDriverName = "scsi"
	imageTypeNoCloud                  imageType      = "nocloud"
	imageTypeConfigDrive              imageType      = "configdrive"
)

// VirtletAnnotations contains parsed values for pod annotations supported
// by Virtlet.
type VirtletAnnotations struct {
	VCPUCount         int
	ImageType         imageType
	MetaData          map[string]interface{}
	UserData          map[string]interface{}
	UserDataOverwrite bool
	UserDataScript    string
	SSHKeys           []string
	DiskDriver        diskDriverName
}

// LoadAnnotations parses map of strings to VirtletAnnotations using provided
// ns value.
func LoadAnnotations(ns string, podAnnotations map[string]string) (*VirtletAnnotations, error) {
	var va VirtletAnnotations
	if err := va.parsePodAnnotations(ns, podAnnotations); err != nil {
		return nil, err
	}
	va.applyDefaults()
	if err := va.validate(); err != nil {
		return nil, err
	}
	return &va, nil
}

func (va *VirtletAnnotations) parsePodAnnotations(ns string, podAnnotations map[string]string) error {
	if podAnnotations[cloudInitUserDataOverwriteKeyName] == "true" {
		va.UserDataOverwrite = true
	}
	if err := va.loadExternalUserData(ns, podAnnotations); err != nil {
		return err
	}

	if vcpuCountStr, found := podAnnotations[vcpuCountAnnotationKeyName]; found {
		var err error
		n, err := strconv.Atoi(vcpuCountStr)
		if err != nil {
			return fmt.Errorf("error parsing cpu count for VM pod (%q)", vcpuCountStr)
		}
		va.VCPUCount = n
	}

	if metaDataStr, found := podAnnotations[cloudInitMetaDataKeyName]; found {
		if err := yaml.Unmarshal([]byte(metaDataStr), &va.MetaData); err != nil {
			return fmt.Errorf("failed to unmarshal cloud-init metadata")
		}
	}

	if userDataStr, found := podAnnotations[cloudInitUserDataKeyName]; found {
		var userData map[string]interface{}
		if err := yaml.Unmarshal([]byte(userDataStr), &userData); err != nil {
			return fmt.Errorf("failed to unmarshal cloud-init userdata")
		}
		if va.UserDataOverwrite {
			va.UserData = userData
		} else {
			va.UserData = utils.Merge(va.UserData, userData).(map[string]interface{})
		}
	}

	va.UserDataScript = podAnnotations[cloudInitUserDataScriptKeyName]

	if sshKeysStr, found := podAnnotations[sshKeysKeyName]; found {
		if va.UserDataOverwrite {
			va.SSHKeys = nil
		}
		keys := strings.Split(sshKeysStr, "\n")
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k != "" {
				va.SSHKeys = append(va.SSHKeys, k)
			}
		}
	}

	va.ImageType = imageType(strings.ToLower(podAnnotations[cloudInitImageType]))
	va.DiskDriver = diskDriverName(podAnnotations[diskDriverKeyName])

	return nil
}

func (va *VirtletAnnotations) applyDefaults() {
	if va.VCPUCount <= 0 {
		va.VCPUCount = 1
	}

	if va.DiskDriver == "" {
		va.DiskDriver = diskDriverScsi
	}

	if va.ImageType == "" {
		va.ImageType = imageTypeNoCloud
	}
}

func (va *VirtletAnnotations) validate() error {
	var errs []string
	if va.VCPUCount > maxVCPUCount {
		errs = append(errs, fmt.Sprintf("vcpu count %d too big, max is %d", va.VCPUCount, maxVCPUCount))
	}

	if va.DiskDriver != diskDriverVirtio && va.DiskDriver != diskDriverScsi {
		errs = append(errs, fmt.Sprintf("bad disk driver %q. Must be either %q or %q", va.DiskDriver, diskDriverVirtio, diskDriverScsi))
	}

	if va.ImageType != imageTypeNoCloud && va.ImageType != imageTypeConfigDrive {
		errs = append(errs, fmt.Sprintf("unknown config image type %q. Must be either %q or %q", va.ImageType, imageTypeNoCloud, imageTypeConfigDrive))
	}

	if errs != nil {
		return fmt.Errorf("bad virtlet annotations. Errors:\n%s", strings.Join(errs, "\n"))
	}

	return nil
}

func (va *VirtletAnnotations) loadExternalUserData(ns string, podAnnotations map[string]string) error {
	if ns == "" {
		return nil
	}
	var clientset *kubernetes.Clientset
	userDataSourceKey := podAnnotations[cloudInitUserDataSourceKeyName]
	if userDataSourceKey != "" {
		var err error
		if clientset == nil {
			clientset, err = utils.GetK8sClientset(nil)
			if err != nil {
				return err
			}
		}
		err = va.loadUserDataFromDataSource(ns, userDataSourceKey, clientset)
		if err != nil {
			return err
		}
	}
	sshKeySourceKey := podAnnotations[sshKeySourceKeyName]
	if sshKeySourceKey != "" {
		var err error
		if clientset == nil {
			clientset, err = utils.GetK8sClientset(nil)
			if err != nil {
				return err
			}
		}
		err = va.loadSSHKeysFromDataSource(ns, sshKeySourceKey, clientset)
		if err != nil {
			return err
		}
	}
	return nil
}

func (va *VirtletAnnotations) loadUserDataFromDataSource(ns, key string, clientset *kubernetes.Clientset) error {
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid %s annotation format. Expected kind/name, but insted got %s", cloudInitUserDataSourceKeyName, key)
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

func (va *VirtletAnnotations) loadSSHKeysFromDataSource(ns, key string, clientset *kubernetes.Clientset) error {
	parts := strings.Split(key, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return fmt.Errorf("invalid %s annotation format. Expected kind/name[/key], but insted got %s", sshKeySourceKeyName, key)
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
		secret, err := clientset.Secrets(ns).Get(sourceName, meta_v1.GetOptions{})
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
		configmap, err := clientset.ConfigMaps(ns).Get(sourceName, meta_v1.GetOptions{})
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
