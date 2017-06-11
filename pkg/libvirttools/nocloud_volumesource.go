/*
Copyright 2016-2017 Mirantis

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
	"os"

	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

// nocloudVolume denotes an ISO image using nocloud format
// that contains cloud-init meta-data nad user-data
type nocloudVolume struct {
	volumeBase
}

func GetNocloudVolume(config *VMConfig, owner VolumeOwner) ([]VMVolume, error) {
	return []VMVolume{
		&nocloudVolume{
			volumeBase{config, owner},
		},
	}, nil
}

func (v *nocloudVolume) Setup() (*libvirtxml.DomainDisk, error) {
	g := NewCloudInitGenerator(v.config)
	isoPath, nocloudDiskDef, err := g.GenerateDisk()
	if err != nil {
		return nil, err
	}
	v.config.TempFile = isoPath
	return nocloudDiskDef, nil
}

func (v *nocloudVolume) Teardown() error {
	if v.config.TempFile == "" {
		return nil
	}
	// don't fail to remove the pod if the file cannot be removed, just warn
	if err := os.Remove(v.config.TempFile); err != nil {
		glog.Warning("Cannot remove temporary nocloud file %q: %v", v.config.TempFile, err)
	}
	return nil
}

// TODO: this file needs a test
