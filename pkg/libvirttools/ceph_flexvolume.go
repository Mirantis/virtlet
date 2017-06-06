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
	"encoding/base64"
	"fmt"
	"strings"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type cephFlexvolumeOptions struct {
	// Type field is needed here so it gets written during
	// remarshalling after removing the contents of 'Secret' field
	Type     string `json:"type"`
	Monitor  string `json:"monitor"`
	Pool     string `json:"pool"`
	Volume   string `json:"volume"`
	Secret   string `json:"secret"`
	User     string `json:"user"`
	Protocol string `json:"protocol"`
}

// cephVolume denotes a Ceph RBD volume
type cephVolume struct {
	volumeBase
	opts *cephFlexvolumeOptions
}

func newCephVolume(volumeName, configPath string, config *VMConfig, owner VolumeOwner) (VMVolume, error) {
	v := &cephVolume{
		volumeBase: volumeBase{config, owner},
	}
	if err := utils.ReadJson(configPath, &v.opts); err != nil {
		return nil, fmt.Errorf("failed to parse ceph flexvolume config %q: %v", configPath, err)
	}
	// Remove the key from flexvolume options to limit exposure.
	// The file itself will be needed to recreate cephVolume during the teardown,
	// but we don't need secret content at that time anymore
	safeOpts := *v.opts
	safeOpts.Secret = ""
	if err := utils.WriteJson(configPath, safeOpts, 0700); err != nil {
		return nil, fmt.Errorf("failed to overwrite ceph flexvolume config %q: %v", configPath, err)
	}
	return v, nil
}

func (v *cephVolume) secretUuid() string {
	return utils.NewUuid5(containerNsUuid, v.config.PodSandboxId)
}

func (v *cephVolume) secretDef() *libvirtxml.Secret {
	return &libvirtxml.Secret{
		Ephemeral: "no",
		Private:   "no",
		UUID:      v.secretUuid(),
		Usage:     &libvirtxml.SecretUsage{Name: v.opts.User, Type: "ceph"},
	}
}

func (v *cephVolume) Setup(virtDev string) (*libvirtxml.DomainDisk, error) {
	secretUuid := v.secretUuid()
	secret, err := v.owner.DomainConnection().LookupSecretByUUIDString(secretUuid)
	ipPortPair := strings.Split(v.opts.Monitor, ":")
	if len(ipPortPair) != 2 {
		return nil, fmt.Errorf("invalid format of ceph monitor setting: %s. Expected ip:port", v.opts.Monitor)
	}

	if err == virt.ErrSecretNotFound {
		secret, err = v.owner.DomainConnection().DefineSecret(v.secretDef())
	}
	if err != nil {
		return nil, fmt.Errorf("error defining ceph secret: %v", err)
	}

	key, err := base64.StdEncoding.DecodeString(v.opts.Secret)
	if err != nil {
		return nil, fmt.Errorf("error decoding ceph secret: %v", err)
	}

	if err := secret.SetValue([]byte(key)); err != nil {
		return nil, fmt.Errorf("error setting value of secret %q: %v", secretUuid, err)
	}

	return &libvirtxml.DomainDisk{
		Type:   "network",
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Auth: &libvirtxml.DomainDiskAuth{
			Username: v.opts.User,
			Secret: &libvirtxml.DomainDiskSecret{
				Type: "ceph",
				UUID: secretUuid,
			},
		},
		Source: &libvirtxml.DomainDiskSource{
			Protocol: "rbd",
			Name:     v.opts.Pool + "/" + v.opts.Volume,
			Hosts: []libvirtxml.DomainDiskSourceHost{
				libvirtxml.DomainDiskSourceHost{
					Name: ipPortPair[0],
					Port: ipPortPair[1],
				},
			},
		},
		Target: &libvirtxml.DomainDiskTarget{Dev: virtDev, Bus: "virtio"},
	}, nil
}

func (v *cephVolume) Teardown() error {
	secret, err := v.owner.DomainConnection().LookupSecretByUUIDString(v.secretUuid())
	switch {
	case err == virt.ErrSecretNotFound:
		// ok, no need to delete the secret
	case err == nil:
		err = secret.Remove()
	}
	if err != nil {
		return fmt.Errorf("error deleting secret %q: %v", v.secretUuid(), err)
	}
	return nil
}

func init() {
	AddFlexvolumeSource("ceph", newCephVolume)
}

// TODO: this file needs a test
