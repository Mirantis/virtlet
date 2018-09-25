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

package types

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	maxVCPUCount                      = 255
	vcpuCountAnnotationKeyName        = "VirtletVCPUCount"
	diskDriverKeyName                 = "VirtletDiskDriver"
	cloudInitMetaDataKeyName          = "VirtletCloudInitMetaData"
	cloudInitUserDataOverwriteKeyName = "VirtletCloudInitUserDataOverwrite"
	cloudInitUserDataKeyName          = "VirtletCloudInitUserData"
	cloudInitUserDataScriptKeyName    = "VirtletCloudInitUserDataScript"
	cloudInitImageType                = "VirtletCloudInitImageType"
	cpuModel                          = "VirtletCPUModel"
	rootVolumeSizeKeyName             = "VirtletRootVolumeSize"
	libvirtCPUSetting                 = "VirtletLibvirtCPUSetting"
	sshKeysKeyName                    = "VirtletSSHKeys"
	chown9pfsMountsKeyName            = "VirtletChown9pfsMounts"
	// CloudInitUserDataSourceKeyName is the name of user data source key in the pod annotations.
	CloudInitUserDataSourceKeyName = "VirtletCloudInitUserDataSource"
	// SSHKeySourceKeyName is the name of ssh key source key in the pod annotations.
	SSHKeySourceKeyName = "VirtletSSHKeySource"

	cloudInitUserDataSourceKeyKeyName      = "VirtletCloudInitUserDataSourceKey"
	cloudInitUserDataSourceEncodingKeyName = "VirtletCloudInitUserDataSourceEncoding"
)

// CloudInitImageType specifies the image type used for cloud-init
type CloudInitImageType string

// CPUModelType specifies cpu model in libvirt domain definition
type CPUModelType string

const (
	// CloudInitImageTypeNoCloud specified nocloud cloud-init image type.
	CloudInitImageTypeNoCloud CloudInitImageType = "nocloud"
	// CloudInitImageTypeConfigDrive specified configdrive cloud-init image type.
	CloudInitImageTypeConfigDrive CloudInitImageType = "configdrive"
	// CPUModelHostModel specifies cpu model needed for nested virtualization
	CPUModelHostModel = "host-model"
)

// DiskDriverName specifies disk driver name supported by Virtlet.
type DiskDriverName string

const (
	// DiskDriverVirtio specifies virtio disk driver.
	DiskDriverVirtio DiskDriverName = "virtio"
	// DiskDriverScsi specifies scsi disk driver.
	DiskDriverScsi DiskDriverName = "scsi"
)

// VirtletAnnotations contains parsed values for pod annotations supported
// by Virtlet.
type VirtletAnnotations struct {
	// Number of virtual CPUs.
	VCPUCount int
	// CPU model.
	CPUModel CPUModelType
	// Cloud-Init image type to use.
	CDImageType CloudInitImageType
	// Cloud-Init metadata.
	MetaData map[string]interface{}
	// Cloud-Init userdata
	UserData map[string]interface{}
	// True if the userdata is overridden.
	UserDataOverwrite bool
	// UserDataScript specifies the script to be used as userdata.
	UserDataScript string
	// SSHKets specifies ssh public keys to use.
	SSHKeys []string
	// DiskDriver specifies the disk driver to use.
	DiskDriver DiskDriverName
	// CPUSetting directly specifies the cpu to use for libvirt.
	CPUSetting *libvirtxml.DomainCPU
	// Root volume size in bytes. Defaults to 0 which means using
	// the size of QCOW2 image). If the value is less then the
	// size of the QCOW2 image, the size of the QCOW2 image is
	// used instead.
	RootVolumeSize int64
	// VirtletChown9pfsMounts indicates if chown is enabled for 9pfs mounts.
	VirtletChown9pfsMounts bool
}

// ExternalDataLoader is a function that loads external data that's specified
// in the pod annotations.
type ExternalDataLoader func(va *VirtletAnnotations, Namespace string, podAnnotations map[string]string) error

var externalDataLoader ExternalDataLoader

// SetExternalDataLoader sets the external data loader function that
// loads external data that's specified in the pod annotations.
func SetExternalDataLoader(loader ExternalDataLoader) {
	externalDataLoader = loader
}

func (va *VirtletAnnotations) applyDefaults() {
	if va.VCPUCount <= 0 {
		va.VCPUCount = 1
	}

	if va.DiskDriver == "" {
		va.DiskDriver = DiskDriverScsi
	}

	if va.CDImageType == "" {
		va.CDImageType = CloudInitImageTypeNoCloud
	}
}

func (va *VirtletAnnotations) validate() error {
	var errs []string
	if va.VCPUCount > maxVCPUCount {
		errs = append(errs, fmt.Sprintf("vcpu count %d too big, max is %d", va.VCPUCount, maxVCPUCount))
	}

	if va.DiskDriver != DiskDriverVirtio && va.DiskDriver != DiskDriverScsi {
		errs = append(errs, fmt.Sprintf("bad disk driver %q. Must be either %q or %q", va.DiskDriver, DiskDriverVirtio, DiskDriverScsi))
	}

	if va.CDImageType != CloudInitImageTypeNoCloud && va.CDImageType != CloudInitImageTypeConfigDrive {
		errs = append(errs, fmt.Sprintf("unknown config image type %q. Must be either %q or %q", va.CDImageType, CloudInitImageTypeNoCloud, CloudInitImageTypeConfigDrive))
	}

	if va.CPUModel != "" && va.CPUModel != CPUModelHostModel {
		errs = append(errs, fmt.Sprintf("unknown cpu model type %q. Must be empty or %q", va.CPUModel, CPUModelHostModel))
	}

	if errs != nil {
		return fmt.Errorf("bad virtlet annotations. Errors:\n%s", strings.Join(errs, "\n"))
	}

	return nil
}

func loadAnnotations(ns string, podAnnotations map[string]string) (*VirtletAnnotations, error) {
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
	if cpuSettingStr, found := podAnnotations[libvirtCPUSetting]; found {
		var cpuSetting libvirtxml.DomainCPU
		if err := yaml.Unmarshal([]byte(cpuSettingStr), &cpuSetting); err != nil {
			return err
		}
		va.CPUSetting = &cpuSetting
	}

	if cpuModelStr, found := podAnnotations[cpuModel]; found {
		va.CPUModel = CPUModelType(cpuModelStr)
	}

	if podAnnotations[cloudInitUserDataOverwriteKeyName] == "true" {
		va.UserDataOverwrite = true
	}
	if externalDataLoader != nil {
		if err := externalDataLoader(va, ns, podAnnotations); err != nil {
			return fmt.Errorf("error loading data via external data loader: %v", err)
		}
	}

	if vcpuCountStr, found := podAnnotations[vcpuCountAnnotationKeyName]; found {
		var err error
		if va.VCPUCount, err = strconv.Atoi(vcpuCountStr); err != nil {
			return fmt.Errorf("error parsing cpu count for VM pod: %q: %v", vcpuCountStr, err)
		}
	}

	if metaDataStr, found := podAnnotations[cloudInitMetaDataKeyName]; found {
		if err := yaml.Unmarshal([]byte(metaDataStr), &va.MetaData); err != nil {
			return fmt.Errorf("failed to unmarshal cloud-init metadata: %v", err)
		}
	}

	if userDataStr, found := podAnnotations[cloudInitUserDataKeyName]; found {
		var userData map[string]interface{}
		if err := yaml.Unmarshal([]byte(userDataStr), &userData); err != nil {
			return fmt.Errorf("failed to unmarshal cloud-init userdata: %v", err)
		}
		if va.UserDataOverwrite {
			va.UserData = userData
		} else {
			va.UserData = utils.Merge(va.UserData, userData).(map[string]interface{})
		}
	}

	va.UserDataScript = podAnnotations[cloudInitUserDataScriptKeyName]

	encoding := "plain"
	if encodingStr, found := podAnnotations[cloudInitUserDataSourceEncodingKeyName]; found {
		encoding = encodingStr
	}
	if key, found := podAnnotations[cloudInitUserDataSourceKeyKeyName]; found {
		data, found := va.UserData[key]
		if !found {
			return fmt.Errorf("user-data script source not found under key %q", key)
		}

		dataStr, ok := data.(string)
		if !ok {
			return fmt.Errorf("failed to read user-data script source from key %q", key)
		}

		switch encoding {
		case "plain":
			va.UserDataScript = dataStr
		case "base64":
			ud, err := base64.StdEncoding.DecodeString(dataStr)
			if err != nil {
				return fmt.Errorf("failed to decode user-data script in base64 encoding: %v", err)
			}
			va.UserDataScript = string(ud)
		default:
			return fmt.Errorf("failed to decode user-data script: %q is unknown encoding", encoding)
		}
	}

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

	va.CDImageType = CloudInitImageType(strings.ToLower(podAnnotations[cloudInitImageType]))
	va.DiskDriver = DiskDriverName(podAnnotations[diskDriverKeyName])

	if rootVolumeSizeStr, found := podAnnotations[rootVolumeSizeKeyName]; found {
		if q, err := resource.ParseQuantity(rootVolumeSizeStr); err != nil {
			return fmt.Errorf("error parsing the root volume size for VM pod: %q: %v", rootVolumeSizeStr, err)
		} else if size, ok := q.AsInt64(); ok {
			va.RootVolumeSize = size
		} else {
			return fmt.Errorf("bad root volume size %q", rootVolumeSizeStr)
		}
	}

	if podAnnotations[chown9pfsMountsKeyName] == "true" {
		va.VirtletChown9pfsMounts = true
	}

	return nil
}
