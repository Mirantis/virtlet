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
	"fmt"
	"strings"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

func cephVolumeHandler(uuidGen UuidGen, targetDir string, opts volumeOpts) (map[string][]byte, error) {
	uuid := uuidGen()
	pairIPPort := strings.Split(opts.Monitor, ":")
	if len(pairIPPort) != 2 {
		return nil, fmt.Errorf("invalid format of ceph monitor setting: %s. Expected in form ip:port", opts.Monitor)
	}

	disk := libvirtxml.DomainDisk{
		Type:   "network",
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Auth: &libvirtxml.DomainDiskAuth{
			Username: opts.User,
			Secret: &libvirtxml.DomainDiskSecret{
				Type: "ceph",
				UUID: uuid,
			},
		},
		Source: &libvirtxml.DomainDiskSource{
			Protocol: "rbd",
			Name:     opts.Pool + "/" + opts.Volume,
			Hosts: []libvirtxml.DomainDiskSourceHost{
				libvirtxml.DomainDiskSourceHost{
					Name: pairIPPort[0],
					Port: pairIPPort[1],
				},
			},
		},
		Target: &libvirtxml.DomainDiskTarget{Bus: "virtio"},
	}
	diskXML, err := xml.MarshalIndent(&disk, "", "  ")
	if err != nil {
		return nil, err
	}

	secret := libvirtxml.Secret{
		Ephemeral: "no",
		Private:   "no",
		UUID:      uuid,
		Usage:     &libvirtxml.SecretUsage{Name: opts.User},
	}
	secretXML, err := secret.Marshal()
	if err != nil {
		return nil, err
	}

	return map[string][]byte{
		// Note: target dev name will be specified by virtlet later when building full domain xml definition
		"disk.xml":   diskXML,
		"secret.xml": []byte(secretXML),
		// Will be removed right after creating appropriate secret in libvirt
		"key": []byte(opts.Secret),
	}, nil
}
