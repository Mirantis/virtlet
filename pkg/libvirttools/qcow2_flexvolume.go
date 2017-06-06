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

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type qcow2VolumeOptions struct {
	Capacity     string `json:"capacity,omitempty"`
	CapacityUnit string `json:"capacityUnit,omitempty"`
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

	if opts.Capacity == "" {
		v.capacity = defaultVolumeCapacity
	} else {
		if v.capacity, err = strconv.Atoi(opts.Capacity); err != nil {
			return nil, fmt.Errorf("qcow2 volume has bad capacity: %v", opts.Capacity)
		}
		if v.capacity < 0 {
			return nil, fmt.Errorf("qcow2 volume has negative capacity %d", v.capacity)
		}
	}

	switch {
	case opts.CapacityUnit == "":
		v.capacityUnit = defaultVolumeCapacityUnit
	case !validCapacityUnit(opts.CapacityUnit):
		return nil, fmt.Errorf("qcow2 has invalid capacity units %q", opts.CapacityUnit)
	default:
		v.capacityUnit = opts.CapacityUnit
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

func validCapacityUnit(unit string) bool {
	for _, item := range capacityUnits {
		if item == unit {
			return true
		}
	}
	return false
}

func init() {
	AddFlexvolumeSource("qcow2", newQCOW2Volume)
}

// TODO: this file needs a test
