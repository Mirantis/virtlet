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
	"path/filepath"
	"strings"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/utils"
)

type rawVolumeOptions struct {
	Path string `json:"path"`
	UUID string `json:"uuid"`
}

func (vo *rawVolumeOptions) validate() error {
	if !strings.HasPrefix(vo.Path, "/dev/") {
		return fmt.Errorf("raw volume path needs to be prefixed by '/dev/', but it's whole value is: %s", vo.Path)
	}
	return nil
}

// rawDeviceVolume denotes a raw device that's made accessible for a VM
type rawDeviceVolume struct {
	volumeBase
	opts *rawVolumeOptions
}

var _ VMVolume = &rawDeviceVolume{}

func newRawDeviceVolume(volumeName, configPath string, config *types.VMConfig, owner volumeOwner) (VMVolume, error) {
	var opts rawVolumeOptions
	if err := utils.ReadJSON(configPath, &opts); err != nil {
		return nil, fmt.Errorf("failed to parse raw volume config %q: %v", configPath, err)
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return &rawDeviceVolume{
		volumeBase: volumeBase{config, owner},
		opts:       &opts,
	}, nil
}

func (v *rawDeviceVolume) verifyRawDeviceWhitelisted(path string) error {
	for _, deviceTemplate := range v.owner.RawDevices() {
		matches, err := filepath.Match("/dev/"+deviceTemplate, path)
		if err != nil {
			return fmt.Errorf("bad raw device whitelist glob pattern '%s': %v", deviceTemplate, err)
		}
		if matches {
			return nil
		}
	}
	return fmt.Errorf("device '%s' not whitelisted on this virtlet node", path)
}

func (v *rawDeviceVolume) UUID() string {
	return v.opts.UUID
}

func (v *rawDeviceVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	if err := v.verifyRawDeviceWhitelisted(v.opts.Path); err != nil {
		return nil, nil, err
	}

	if err := verifyRawDeviceAccess(v.opts.Path); err != nil {
		return nil, nil, err
	}
	return &libvirtxml.DomainDisk{
		Device: "disk",
		Source: &libvirtxml.DomainDiskSource{Block: &libvirtxml.DomainDiskSourceBlock{Dev: v.opts.Path}},
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
	}, nil, nil
}

func init() {
	addFlexvolumeSource("raw", newRawDeviceVolume)
}

// TODO: this file needs a test
