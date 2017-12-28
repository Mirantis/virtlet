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

var nocloudIsoDir = "/var/lib/virtlet/nocloud"

// nocloudVolume denotes an ISO image using nocloud format
// that contains cloud-init meta-data nad user-data
type nocloudVolume struct {
	volumeBase
}

var _ VMVolume = &nocloudVolume{}

func GetNocloudVolume(config *VMConfig, owner VolumeOwner) ([]VMVolume, error) {
	return []VMVolume{
		&nocloudVolume{
			volumeBase{config, owner},
		},
	}, nil
}

func (v *nocloudVolume) Uuid() string { return "" }

func (v *nocloudVolume) cloudInitGenerator() *CloudInitGenerator {
	return NewCloudInitGenerator(v.config, nocloudIsoDir)
}

func (v *nocloudVolume) Setup() (*libvirtxml.DomainDisk, error) {
	return v.cloudInitGenerator().DiskDef(), nil
}

func (v *nocloudVolume) WriteImage(volumeMap diskPathMap) error {
	return v.cloudInitGenerator().GenerateImage(volumeMap)
}

func (v *nocloudVolume) Teardown() error {
	isoPath := v.cloudInitGenerator().IsoPath()
	if err := os.Remove(isoPath); err != nil && !os.IsNotExist(err) {
		glog.Warningf("Cannot remove temporary nocloud file %q: %v", isoPath, err)
	}
	return nil
}

// SetNocloudIsoDir sets a directory for nocloud iso dir.
// It can be useful in tests
func SetNocloudIsoDir(dir string) {
	nocloudIsoDir = dir
}

// TODO: this file needs a test
