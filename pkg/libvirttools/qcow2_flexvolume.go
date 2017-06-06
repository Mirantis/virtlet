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
	"regexp"
	"strconv"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
)

const (
	defaultVolumeCapacity     = 1024
	defaultVolumeCapacityUnit = "MB"
)

var capacityUnits []string = []string{
	// https://libvirt.org/formatstorage.html#StorageVolFirst
	"B", "bytes", "KB", "K", "KiB", "MB", "M", "MiB", "GB", "G",
	"GiB", "TB", "T", "TiB", "PB", "P", "PiB", "EB", "E", "EiB",
}

var capacityRx = regexp.MustCompile(`^\s*(\d+)\s*(\S*)\s*$`)

type qcow2VolumeOptions struct {
	Capacity string `json:"capacity,omitempty"`
}

// qcow2Volume denotes a volume in QCOW2 format
type qcow2Volume struct {
	volumeBase
	capacity     int
	capacityUnit string
	name         string
}

func newQCOW2Volume(volumeName, configPath string, config *VMConfig, owner VolumeOwner) (VMVolume, error) {
	v := &qcow2Volume{
		volumeBase: volumeBase{config, owner},
		name:       volumeName,
	}
	var err error
	var opts qcow2VolumeOptions
	if err = utils.ReadJson(configPath, &opts); err != nil {
		return nil, fmt.Errorf("failed to parse qcow2 volume config %q: %v", configPath, err)
	}

	v.capacity, v.capacityUnit, err = parseCapacityStr(opts.Capacity)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (v *qcow2Volume) volumeName() string {
	return v.config.DomainUUID + "-" + v.name
}

func (v *qcow2Volume) createQCOW2Volume(name string, capacity uint64, capacityUnit string) (virt.VirtStorageVolume, error) {
	return v.owner.StoragePool().CreateStorageVol(&libvirtxml.StorageVolume{
		Name:       name,
		Allocation: &libvirtxml.StorageVolumeSize{Value: 0},
		Capacity:   &libvirtxml.StorageVolumeSize{Unit: capacityUnit, Value: capacity},
		Target:     &libvirtxml.StorageVolumeTarget{Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"}},
	})
}

func (v *qcow2Volume) Setup(virtDev string) (*libvirtxml.DomainDisk, error) {
	vol, err := v.createQCOW2Volume(v.volumeName(), uint64(v.capacity), v.capacityUnit)
	if err != nil {
		return nil, fmt.Errorf("error during creation of volume '%s' with virtlet description %s: %v", v.volumeName(), v.name, err)
	}

	path, err := vol.Path()
	if err != nil {
		return nil, err
	}

	err = vol.Format()
	if err != nil {
		return nil, err
	}

	return &libvirtxml.DomainDisk{
		Type:   "file",
		Device: "disk",
		Source: &libvirtxml.DomainDiskSource{File: path},
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "qcow2"},
		Target: &libvirtxml.DomainDiskTarget{Dev: virtDev, Bus: "virtio"},
	}, nil
}

func (v *qcow2Volume) Teardown() error {
	return v.owner.StoragePool().RemoveVolumeByName(v.volumeName())
}

func parseCapacityStr(capacityStr string) (int, string, error) {
	if capacityStr == "" {
		return defaultVolumeCapacity, defaultVolumeCapacityUnit, nil
	}

	subs := capacityRx.FindStringSubmatch(capacityStr)
	if subs == nil {
		return 0, "", fmt.Errorf("invalid capacity spec: %q", capacityStr)
	}
	capacity, err := strconv.Atoi(subs[1])
	if err != nil {
		return 0, "", fmt.Errorf("invalid capacity spec: %q", capacityStr)
	}
	capacityUnit := subs[2]
	if capacityUnit == "" {
		return capacity, defaultVolumeCapacityUnit, nil
	} else {
		for _, item := range capacityUnits {
			if item == capacityUnit {
				return capacity, capacityUnit, nil
			}
		}
	}
	return 0, "", fmt.Errorf("invalid capacity unit: %q", capacityUnit)
}

func init() {
	AddFlexvolumeSource("qcow2", newQCOW2Volume)
}

// TODO: this file needs a test
