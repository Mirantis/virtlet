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

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/virt"
)

// rootVolume denotes the root disk of the VM
type rootVolume struct {
	volumeBase
}

var _ VMVolume = &rootVolume{}

// GetRootVolume returns volume source for root volume clone.
func GetRootVolume(config *types.VMConfig, owner volumeOwner) ([]VMVolume, error) {
	return []VMVolume{
		&rootVolume{
			volumeBase{config, owner},
		},
	}, nil
}

func (v *rootVolume) volumeName() string {
	return "virtlet_root_" + v.config.DomainUUID
}

func (v *rootVolume) createVolume() (virt.StorageVolume, error) {
	imagePath, virtualSize, err := v.owner.ImageManager().GetImagePathAndVirtualSize(v.config.Image)
	if err != nil {
		return nil, err
	}

	storagePool, err := v.owner.StoragePool()
	if err != nil {
		return nil, err
	}
	return storagePool.CreateStorageVol(&libvirtxml.StorageVolume{
		Type: "file",
		Name: v.volumeName(),
		Allocation: &libvirtxml.StorageVolumeSize{
			Unit:  "b",
			Value: 0,
		},
		Capacity: &libvirtxml.StorageVolumeSize{
			Unit:  "b",
			Value: virtualSize,
		},
		Target: &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"},
		},
		BackingStore: &libvirtxml.StorageVolumeBackingStore{
			Path:   imagePath,
			Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"},
		},
	})
}

func (v *rootVolume) UUID() string { return "" }

func (v *rootVolume) Setup() (*libvirtxml.DomainDisk, error) {
	vol, err := v.createVolume()
	if err != nil {
		return nil, err
	}
	volPath, err := vol.Path()
	if err != nil {
		return nil, fmt.Errorf("error getting root volume path: %v", err)
	}

	return &libvirtxml.DomainDisk{
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "qcow2"},
		Source: &libvirtxml.DomainDiskSource{File: &libvirtxml.DomainDiskSourceFile{File: volPath}},
	}, nil
}

func (v *rootVolume) Teardown() error {
	storagePool, err := v.owner.StoragePool()
	if err != nil {
		return err
	}
	return storagePool.RemoveVolumeByName(v.volumeName())
}
