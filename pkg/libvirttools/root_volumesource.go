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

// rootVolume denotes the root disk of the VM
type rootVolume struct {
	volumeBase
}

func GetRootVolume(config *VMConfig, owner VolumeOwner) ([]VMVolume, error) {
	return []VMVolume{
		&rootVolume{
			volumeBase{config, owner},
		},
	}, nil
}

func (v *rootVolume) cloneName() string {
	return "root_" + v.config.DomainUUID
}

func (v *rootVolume) cloneVolume(name string, from virt.VirtStorageVolume) (virt.VirtStorageVolume, error) {
	return v.owner.StoragePool().CreateStorageVolClone(&libvirtxml.StorageVolume{
		Name: name,
		Type: "file",
		Target: &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"},
		},
	}, from)
}

func (v *rootVolume) Uuid() string { return "" }

func (v *rootVolume) Setup(volumeMap map[string]string) (*libvirtxml.DomainDisk, error) {
	imageVolume, err := v.owner.ImageManager().GetImageVolume(v.config.Image)
	if err != nil {
		return nil, err
	}

	vol, err := v.cloneVolume(v.cloneName(), imageVolume)
	if err != nil {
		return nil, err
	}
	volPath, err := vol.Path()
	if err != nil {
		return nil, fmt.Errorf("error getting root volume path: %v", err)
	}

	return &libvirtxml.DomainDisk{
		Type:   "file",
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "qcow2"},
		Source: &libvirtxml.DomainDiskSource{File: volPath},
	}, nil
}

func (v *rootVolume) Teardown() error {
	return v.owner.StoragePool().RemoveVolumeByName(v.cloneName())
}

// TODO: this file needs a test
