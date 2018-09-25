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

package libvirttools

import (
	"path/filepath"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

// blockBolume denotes a block device that's made accessible inside the VM
type blockVolume struct {
	volumeBase
	dev types.VMVolumeDevice
}

var _ VMVolume = &blockVolume{}

func (v *blockVolume) UUID() string {
	return v.dev.UUID()
}

func (v *blockVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	// we need to follow the symlinks as only devices under /dev
	// will be chown'ed properly by QEMU
	hostPath, err := filepath.EvalSymlinks(v.dev.HostPath)
	if err != nil {
		return nil, nil, err
	}
	return &libvirtxml.DomainDisk{
		Device: "disk",
		Source: &libvirtxml.DomainDiskSource{Block: &libvirtxml.DomainDiskSourceBlock{Dev: hostPath}},
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
	}, nil, nil
}

func (v *blockVolume) Teardown() error {
	return nil
}

// GetBlockVolumes returns VMVolume objects for block devices that are
// passed to the pod.
func GetBlockVolumes(config *types.VMConfig, owner volumeOwner) ([]VMVolume, error) {
	var vols []VMVolume
	for _, dev := range config.VolumeDevices {
		if dev.IsRoot() {
			continue
		}
		vols = append(vols, &blockVolume{
			volumeBase: volumeBase{config, owner},
			dev:        dev,
		})
	}
	return vols, nil
}
