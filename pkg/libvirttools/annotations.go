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
)

const (
	maxVCPUCount                      = 255
	defaultVolumeCapacity             = 1024
	defaultVolumeCapacityUnit         = "MB"
	VirtletVCPUCountAnnotationKeyName = "VirtletVCPUCount"
	VirtletVolumesAnnotationKeyName   = "VirtletVolumes"
)

type VirtletAnnotations struct {
	VCPUCount int
	Volumes   []*VirtletVolume
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
	if vcpuCountStr, found := podAnnotations[VirtletVCPUCountAnnotationKeyName]; found {
		var err error
		n, err := strconv.Atoi(vcpuCountStr)
		if err != nil {
			return fmt.Errorf("error parsing cpu count for VM pod (%q)", vcpuCountStr)
		}
		va.VCPUCount = n
	}

	if volumesStr, found := podAnnotations[VirtletVolumesAnnotationKeyName]; found {
		if err := json.Unmarshal([]byte(volumesStr), &va.Volumes); err != nil {
			return fmt.Errorf("failed to unmarshal virtlet volumes: %v", err)
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
