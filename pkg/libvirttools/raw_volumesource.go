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

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

// rawDeviceVolume denotes a raw device that's made accessible for a VM
type rawDeviceVolume struct {
	volumeBase
	devPath string
}

func newRawDeviceVolume(devPath string, config *VMConfig, owner VolumeOwner) VMVolume {
	return &rawDeviceVolume{
		volumeBase: volumeBase{config, owner},
		devPath:    devPath,
	}
}

func (v *rawDeviceVolume) verifyRawDeviceWhitelisted(path string) error {
	for _, deviceTemplate := range v.owner.RawDevices() {
		devicePaths, err := filepath.Glob("/dev/" + deviceTemplate)
		if err != nil {
			return fmt.Errorf("error in raw device whitelist glob pattern '%s': %v", deviceTemplate, err)
		}
		for _, devicePath := range devicePaths {
			if path == devicePath {
				return nil
			}
		}
	}
	return fmt.Errorf("device '%s' not whitelisted on this virtlet node", path)
}

func (v *rawDeviceVolume) Setup(virtDev string) (*libvirtxml.DomainDisk, error) {
	if err := v.verifyRawDeviceWhitelisted(v.devPath); err != nil {
		return nil, err
	}

	if err := verifyRawDeviceAccess(v.devPath); err != nil {
		return nil, err
	}
	return &libvirtxml.DomainDisk{
		Type:   "block",
		Device: "disk",
		Source: &libvirtxml.DomainDiskSource{Device: v.devPath},
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Target: &libvirtxml.DomainDiskTarget{Dev: virtDev, Bus: "virtio"},
	}, nil
}

func (v *rawDeviceVolume) Teardown() error { return nil }

// TODO: this file needs a test
