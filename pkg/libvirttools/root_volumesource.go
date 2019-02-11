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
	var vol VMVolume
	rootDev := config.RootVolumeDevice()
	if rootDev != nil {
		vol = &persistentRootVolume{
			volumeBase: volumeBase{config, owner},
			dev:        *rootDev,
		}
	} else {
		vol = &rootVolume{
			volumeBase{config, owner},
		}
	}
	return []VMVolume{vol}, nil
}

func (v *rootVolume) volumeName() string {
	return "virtlet_root_" + v.config.DomainUUID
}

func (v *rootVolume) createVolume() (virt.StorageVolume, error) {
	imagePath, _, virtualSize, err := v.owner.ImageManager().GetImagePathDigestAndVirtualSize(v.config.Image)
	if err != nil {
		return nil, err
	}

	if v.config.ParsedAnnotations != nil && v.config.ParsedAnnotations.RootVolumeSize > 0 &&
		uint64(v.config.ParsedAnnotations.RootVolumeSize) > virtualSize {
		virtualSize = uint64(v.config.ParsedAnnotations.RootVolumeSize)
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

func (v *rootVolume) IsDisk() bool { return true }

func (v *rootVolume) UUID() string { return "" }

func (v *rootVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	vol, err := v.createVolume()
	if err != nil {
		return nil, nil, err
	}

	volPath, err := vol.Path()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting root volume path: %v", err)
	}

	if len(v.config.ParsedAnnotations.InjectedFiles) > 0 {
		if err := v.owner.StorageConnection().PutFiles(volPath, v.config.ParsedAnnotations.InjectedFiles); err != nil {
			return nil, nil, fmt.Errorf("error adding files to rootfs: %v", err)
		}
	}

	return &libvirtxml.DomainDisk{
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "qcow2"},
		Source: &libvirtxml.DomainDiskSource{File: &libvirtxml.DomainDiskSourceFile{File: volPath}},
	}, nil, nil
}

func (v *rootVolume) Teardown() error {
	storagePool, err := v.owner.StoragePool()
	if err != nil {
		return err
	}
	return storagePool.RemoveVolumeByName(v.volumeName())
}
