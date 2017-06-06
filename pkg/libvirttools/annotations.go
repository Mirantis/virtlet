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
)

const (
	maxVCPUCount                      = 255
	VCPUCountAnnotationKeyName        = "VirtletVCPUCount"
	CloudInitMetaDataKeyName          = "VirtletCloudInitMetaData"
	CloudInitUserDataKeyName          = "VirtletCloudInitUserData"
	CloudInitUserDataOverwriteKeyName = "VirtletCloudInitUserDataOverwrite"
	CloudInitUserDataScriptKeyName    = "VirtletCloudInitUserDataScript"
	SSHKeysKeyName                    = "VirtletSSHKeys"
)

type VirtletAnnotations struct {
	VCPUCount         int
	MetaData          map[string]interface{}
	UserData          map[string]interface{}
	UserDataOverwrite bool
	UserDataScript    string
	SSHKeys           []string
}

func LoadAnnotations(podAnnotations map[string]string) (*VirtletAnnotations, error) {
	var va VirtletAnnotations
	if err := va.parsePodAnnotations(podAnnotations); err != nil {
		return nil, err
	}
	va.applyDefaults()
	if err := va.validate(); err != nil {
		return nil, err
	}
	return &va, nil
}

func (va *VirtletAnnotations) parsePodAnnotations(podAnnotations map[string]string) error {
	if vcpuCountStr, found := podAnnotations[VCPUCountAnnotationKeyName]; found {
		var err error
		n, err := strconv.Atoi(vcpuCountStr)
		if err != nil {
			return fmt.Errorf("error parsing cpu count for VM pod (%q)", vcpuCountStr)
		}
		va.VCPUCount = n
	}

	if metaDataStr, found := podAnnotations[CloudInitMetaDataKeyName]; found {
		if err := yaml.Unmarshal([]byte(metaDataStr), &va.MetaData); err != nil {
			return fmt.Errorf("failed to unmarshal cloud-init metadata")
		}
	}

	if userDataStr, found := podAnnotations[CloudInitUserDataKeyName]; found {
		if err := yaml.Unmarshal([]byte(userDataStr), &va.UserData); err != nil {
			return fmt.Errorf("failed to unmarshal cloud-init userdata")
		}
	}

	if podAnnotations[CloudInitUserDataOverwriteKeyName] == "true" {
		va.UserDataOverwrite = true
	}

	va.UserDataScript = podAnnotations[CloudInitUserDataScriptKeyName]

	if sshKeysStr, found := podAnnotations[SSHKeysKeyName]; found {
		keys := strings.Split(sshKeysStr, "\n")
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k != "" {
				va.SSHKeys = append(va.SSHKeys, k)
			}
		}
	}

	return nil
}

func (va *VirtletAnnotations) applyDefaults() {
	if va.VCPUCount <= 0 {
		va.VCPUCount = 1
	}
}

func (va *VirtletAnnotations) validate() error {
	var errs []string
	if va.VCPUCount > maxVCPUCount {
		errs = append(errs, fmt.Sprintf("vcpu count %d too big, max is %d", va.VCPUCount, maxVCPUCount))
	}

	if errs != nil {
		return fmt.Errorf("bad virtlet annotations. Errors:\n%s", strings.Join(errs, "\n"))
	}

	return nil
}
