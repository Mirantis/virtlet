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

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

var configIsoDir = "/var/lib/virtlet/config"

// configVolume denotes an ISO image using config format
// that contains cloud-init meta-data and user-data
type configVolume struct {
	volumeBase
}

var _ VMVolume = &configVolume{}

// GetConfigVolume returns a config volume source which will produce an ISO
// image with CloudInit compatible configuration data.
func GetConfigVolume(config *types.VMConfig, owner volumeOwner) ([]VMVolume, error) {
	return []VMVolume{
		&configVolume{
			volumeBase{config, owner},
		},
	}, nil
}

func (v *configVolume) UUID() string { return "" }

func (v *configVolume) cloudInitGenerator() *CloudInitGenerator {
	return NewCloudInitGenerator(v.config, configIsoDir)
}

func (v *configVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	return v.cloudInitGenerator().DiskDef(),nil, nil
}

func (v *configVolume) WriteImage(volumeMap diskPathMap) error {
	return v.cloudInitGenerator().GenerateImage(volumeMap)
}

func (v *configVolume) Teardown() error {
	isoPath := v.cloudInitGenerator().IsoPath()
	if err := os.Remove(isoPath); err != nil && !os.IsNotExist(err) {
		glog.Warningf("Cannot remove temporary config file %q: %v", isoPath, err)
	}
	return nil
}

// SetConfigIsoDir sets a directory for config iso dir.
// It can be useful in tests
func SetConfigIsoDir(dir string) {
	configIsoDir = dir
}

// TODO: this file needs a test
