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

const (
	// FIXME: make this configurable
	nocloudIsoDir = "/var/lib/virtlet/nocloud"
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

func (v *nocloudVolume) Uuid() string { return "" }

func (v *nocloudVolume) Setup(volumeMap map[string]string) (*libvirtxml.DomainDisk, error) {
	g := NewCloudInitGenerator(v.config, volumeMap, nocloudIsoDir)
	nocloudDiskDef, err := g.GenerateDisk()
	if err != nil {
		return nil, err
	}
	return nocloudDiskDef, nil
}

func (v *nocloudVolume) Teardown() error {
	isoPath := NewCloudInitGenerator(v.config, nil, nocloudIsoDir).IsoPath()
	if err := os.Remove(isoPath); err != nil && !os.IsNotExist(err) {
		glog.Warningf("Cannot remove temporary nocloud file %q: %v", isoPath, err)
	}
	return nil
}

// TODO: this file needs a test
