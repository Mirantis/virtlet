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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	// use this instead of "gopkg.in/yaml.v2" so we don't get
	// map[interface{}]interface{} when unmarshalling cloud-init data
	"github.com/ghodss/yaml"
)

const (
	maxVCPUCount                      = 255
	defaultVolumeCapacity             = 1024
	defaultVolumeCapacityUnit         = "MB"
	VCPUCountAnnotationKeyName        = "VirtletVCPUCount"
	VolumesAnnotationKeyName          = "VirtletVolumes"
	CloudInitMetaDataKeyName          = "VirtletCloudInitMetaData"
	CloudInitUserDataKeyName          = "VirtletCloudInitUserData"
	CloudInitUserDataOverwriteKeyName = "VirtletCloudInitUserDataOverwrite"
	CloudInitUserDataScriptKeyName    = "VirtletCloudInitUserDataScript"
	SSHKeysKeyName                    = "VirtletSSHKeys"
)

var capacityUnits []string = []string{
	// https://libvirt.org/formatstorage.html#StorageVolFirst
	"B", "bytes", "KB", "K", "KiB", "MB", "M", "MiB", "GB", "G",
	"GiB", "TB", "T", "TiB", "PB", "P", "PiB", "EB", "E", "EiB",
}

type VirtletAnnotations struct {
	VCPUCount         int
	Volumes           []*VirtletVolume
	MetaData          map[string]interface{}
	UserData          map[string]interface{}
	UserDataOverwrite bool
	UserDataScript    string
	SSHKeys           []string
}

type VirtletVolume struct {
	Name         string `json:"Name"`
	Format       string `json:"Format"`
	Capacity     int    `json:"Capacity,string,omitempty"`
	CapacityUnit string `json:"CapacityUnit,omitempty"`
	Path         string `json:"Path,omitempty"`
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

	if volumesStr, found := podAnnotations[VolumesAnnotationKeyName]; found {
		if err := json.Unmarshal([]byte(volumesStr), &va.Volumes); err != nil {
			return fmt.Errorf("failed to unmarshal virtlet volumes: %v", err)
		}
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
	for _, vol := range va.Volumes {
		vol.applyDefaults()
	}
}

func (va *VirtletAnnotations) validate() error {
	var errs []string
	if va.VCPUCount > maxVCPUCount {
		errs = append(errs, fmt.Sprintf("vcpu count %d too big, max is %d", va.VCPUCount, maxVCPUCount))
	}

	for _, vol := range va.Volumes {
		if err := vol.validate(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if errs != nil {
		return fmt.Errorf("bad virtlet annotations. Errors:\n%s", strings.Join(errs, "\n"))
	}

	return nil
}

func (vol *VirtletVolume) applyDefaults() {
	if vol.Format == "" {
		vol.Format = "qcow2"
	}
	if vol.Format == "qcow2" {
		if vol.Capacity == 0 {
			vol.Capacity = defaultVolumeCapacity
		}
		if vol.CapacityUnit == "" {
			vol.CapacityUnit = defaultVolumeCapacityUnit
		}
	}
}

func (vol *VirtletVolume) validate() error {
	if vol.Name == "" {
		return errors.New("volume name is mandatory")
	}

	switch vol.Format {
	case "qcow2":
		if vol.Path != "" {
			return fmt.Errorf("qcow2 volume should not have Path but it has it set to: %s", vol.Path)
		}
		if vol.Capacity < 0 {
			return fmt.Errorf("qcow2 volume has negative capacity %d", vol.Capacity)
		}
		if !validCapacityUnit(vol.CapacityUnit) {
			return fmt.Errorf("qcow2 has invalid capacity units %q", vol.CapacityUnit)
		}
	case "rawDevice":
		if vol.Capacity != 0 {
			return fmt.Errorf("raw volume should not have Capacity, but it has it equal to: %d", vol.Capacity)
		}
		if vol.CapacityUnit != "" {
			return fmt.Errorf("raw volume should not have CapacityUnit, but it has it set to: %s", vol.CapacityUnit)
		}
		if !strings.HasPrefix(vol.Path, "/dev/") {
			return fmt.Errorf("raw volume Path needs to be prefixed by '/dev/', but it's whole value is: ", vol.Path)
		}
	default:
		return fmt.Errorf("unsupported volume format: %s", vol.Format)
	}

	return nil
}

func validCapacityUnit(unit string) bool {
	for _, item := range capacityUnits {
		if item == unit {
			return true
		}
	}
	return false
}
