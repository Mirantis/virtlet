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

package flexvolume

import (
	"encoding/xml"
	"path/filepath"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

func noCloudVolumeHandler(uuidGen UuidGen, targetDir string, opts volumeOpts) (map[string][]byte, error) {
	isoPath := filepath.Join(targetDir, "cidata.iso")

	disk := libvirtxml.DomainDisk{
		Type:     "file",
		Device:   "disk",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: isoPath},
		Target:   &libvirtxml.DomainDiskTarget{Bus: "virtio"},
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	}

	diskXML, err := xml.MarshalIndent(&disk, "", "  ")
	if err != nil {
		return nil, err
	}

	return map[string][]byte{
		"disk.xml":            diskXML,
		"cidata.cd/meta-data": []byte(opts.MetaData),
		"cidata.cd/user-data": []byte(opts.UserData),
	}, nil
}
