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

const (
	secretNsUuid = "67b7fb47-7735-4b64-86d2-6d062d121966"
)

type ImageManager interface {
	GetImageVolume(imageName string) (virt.VirtStorageVolume, error)
}

type VolumeOwner interface {
	StoragePool() virt.VirtStoragePool
	DomainConnection() virt.VirtDomainConnection
	ImageManager() ImageManager
	RawDevices() []string
	KubeletRootDir() string
}

type VMVolumeSource func(config *VMConfig, owner VolumeOwner) ([]VMVolume, error)

type VMVolume interface {
	Setup(virtDev string) (*libvirtxml.DomainDisk, error)
	Teardown() error
}

type volumeBase struct {
	config *VMConfig
	owner  VolumeOwner
}

// TODO: this should be converted to flexvolumes
func GetVolumesFromAnnotations(config *VMConfig, owner VolumeOwner) ([]VMVolume, error) {
	var vs []VMVolume
	for _, vv := range config.ParsedAnnotations.Volumes {
		switch vv.Format {
		case "qcow2":
			vs = append(vs, newQCOW2Volume(vv, config, owner))
		case "rawDevice":
			vs = append(vs, newRawDeviceVolume(vv.Path, config, owner))
		default:
			return nil, fmt.Errorf("bad volume format %q", vv.Format)
		}
	}
	return vs, nil
}

func CombineVMVolumeSources(srcs ...VMVolumeSource) VMVolumeSource {
	return func(config *VMConfig, owner VolumeOwner) ([]VMVolume, error) {
		var vols []VMVolume
		for _, src := range srcs {
			vs, err := src(config, owner)
			if err != nil {
				return nil, err
			}
			vols = append(vols, vs...)
		}
		return vols, nil
	}
}
