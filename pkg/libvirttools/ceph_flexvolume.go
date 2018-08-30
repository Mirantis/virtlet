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

	"github.com/golang/glog"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
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
	UUID     string `json:"uuid"`
}

// cephVolume denotes a Ceph RBD volume
type cephVolume struct {
	volumeBase
	volumeName string
	opts       *cephFlexvolumeOptions
}

var _ VMVolume = &cephVolume{}

func newCephVolume(volumeName, configPath string, config *types.VMConfig, owner volumeOwner) (VMVolume, error) {
	v := &cephVolume{
		volumeBase: volumeBase{config, owner},
	}
	if err := utils.ReadJSON(configPath, &v.opts); err != nil {
		return nil, fmt.Errorf("failed to parse ceph flexvolume config %q: %v", configPath, err)
	}
	v.volumeName = volumeName
	// Remove the key from flexvolume options to limit exposure.
	// The file itself will be needed to recreate cephVolume during the teardown,
	// but we don't need secret content at that time anymore
	safeOpts := *v.opts
	safeOpts.Secret = ""
	if err := utils.WriteJSON(configPath, safeOpts, 0700); err != nil {
		return nil, fmt.Errorf("failed to overwrite ceph flexvolume config %q: %v", configPath, err)
	}
	return v, nil
}

func (v *cephVolume) secretUsageName() string {
	return v.opts.User + "-" + utils.NewUUID5(ContainerNsUUID, v.config.PodSandboxID) + "-" + v.volumeName
}

func (v *cephVolume) secretDef() *libvirtxml.Secret {
	return &libvirtxml.Secret{
		Ephemeral: "no",
		Private:   "no",
		// Both secret UUID and Usage name must be unique across all definitions
		// As Usage name is a string and can be used to lookup secret
		// it's more convenient to use it for manipulating secrets
		// and preserve using UUIDv5 as part of value
		// UUID value is generated randomly
		UUID:  utils.NewUUID(),
		Usage: &libvirtxml.SecretUsage{Name: v.secretUsageName(), Type: "ceph"},
	}
}

func (v *cephVolume) UUID() string {
	return v.opts.UUID
}

func (v *cephVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	ipPortPair := strings.Split(v.opts.Monitor, ":")
	if len(ipPortPair) != 2 {
		return nil, nil, fmt.Errorf("invalid format of ceph monitor setting: %s. Expected ip:port", v.opts.Monitor)
	}

	secret, err := v.owner.DomainConnection().DefineSecret(v.secretDef())
	if err != nil {
		return nil, nil, fmt.Errorf("error defining ceph secret: %v", err)
	}

	key, err := base64.StdEncoding.DecodeString(v.opts.Secret)
	if err != nil {
		return nil, nil, fmt.Errorf("error decoding ceph secret: %v", err)
	}

	if err := secret.SetValue([]byte(key)); err != nil {
		return nil, nil, fmt.Errorf("error setting value of secret %q: %v", v.secretUsageName(), err)
	}

	return &libvirtxml.DomainDisk{
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Auth: &libvirtxml.DomainDiskAuth{
			Username: v.opts.User,
			Secret: &libvirtxml.DomainDiskSecret{
				Type:  "ceph",
				Usage: v.secretUsageName(),
			},
		},
		Source: &libvirtxml.DomainDiskSource{
			Network: &libvirtxml.DomainDiskSourceNetwork{
				Protocol: "rbd",
				Name:     v.opts.Pool + "/" + v.opts.Volume,
				Hosts: []libvirtxml.DomainDiskSourceHost{
					{
						Name: ipPortPair[0],
						Port: ipPortPair[1],
					},
				},
			},
		},
	}, nil, nil
}

func (v *cephVolume) Teardown() error {
	secret, err := v.owner.DomainConnection().LookupSecretByUsageName("ceph", v.secretUsageName())
	switch {
	case err == virt.ErrSecretNotFound:
		// ok, no need to delete the secret
		glog.V(3).Infof("No secret with usage name %q for ceph volume was found", v.secretUsageName())
		return nil
	case err == nil:
		glog.V(3).Infof("Removing secret with usage name: %q", v.secretUsageName())
		err = secret.Remove()
	}
	if err != nil {
		return fmt.Errorf("error deleting secret with usage name %q: %v", v.secretUsageName(), err)
	}
	return nil
}

func init() {
	addFlexvolumeSource("ceph", newCephVolume)
}

// TODO: this file needs a test
