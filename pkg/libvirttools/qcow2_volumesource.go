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

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt"
)

// qcow2Volume denotes a volume in QCOW2 format
type qcow2Volume struct {
	volumeBase
	spec *VirtletVolume
}

func newQCOW2Volume(spec *VirtletVolume, config *VMConfig, owner VolumeOwner) VMVolume {
	return &qcow2Volume{
		volumeBase: volumeBase{config, owner},
		spec:       spec,
	}
}

func (v *qcow2Volume) volumeName() string {
	return v.config.DomainUUID + "-" + v.spec.Name
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
	vol, err := v.createQCOW2Volume(v.volumeName(), uint64(v.spec.Capacity), v.spec.CapacityUnit)
	if err != nil {
		return nil, fmt.Errorf("error during creation of volume '%s' with virtlet description %s: %v", v.volumeName(), v.spec.Name, err)
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

// TODO: this file needs a test
