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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/utils"
)

type CloudInitGenerator struct {
	config *VMConfig
}

func NewCloudInitGenerator(config *VMConfig) *CloudInitGenerator {
	return &CloudInitGenerator{config}
}

func (g *CloudInitGenerator) generateMetaData() ([]byte, error) {
	m := map[string]interface{}{
		"instance-id":    fmt.Sprintf("%s.%s", g.config.PodName, g.config.PodNamespace),
		"local-hostname": g.config.PodName,
	}
	if len(g.config.ParsedAnnotations.SSHKeys) != 0 {
		var keys []string
		for _, key := range g.config.ParsedAnnotations.SSHKeys {
			keys = append(keys, key)
		}
		m["public-keys"] = keys
	}
	for k, v := range g.config.ParsedAnnotations.MetaData {
		m[k] = v
	}
	r, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("error marshaling meta-data: %v", err)
	}
	return r, nil
}

func (g *CloudInitGenerator) generateUserData() ([]byte, error) {
	if g.config.ParsedAnnotations.UserDataScript != "" {
		return []byte(g.config.ParsedAnnotations.UserDataScript), nil
	}
	r := []byte{}
	if len(g.config.ParsedAnnotations.UserData) != 0 {
		var err error
		r, err = yaml.Marshal(g.config.ParsedAnnotations.UserData)
		if err != nil {
			return nil, fmt.Errorf("error marshalling user-data: %v", err)
		}
	}
	return []byte("#cloud-config\n" + string(r)), nil
}

func (g *CloudInitGenerator) GenerateDisk() (string, *libvirtxml.DomainDisk, error) {
	tmpDir, err := ioutil.TempDir("", "nocloud-")
	if err != nil {
		return "", nil, fmt.Errorf("can't create temp dir for nocloud: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var metaData, userData []byte
	metaData, err = g.generateMetaData()
	if err == nil {
		userData, err = g.generateUserData()
	}
	if err != nil {
		return "", nil, err
	}

	if err := utils.WriteFiles(tmpDir, map[string][]byte{
		"user-data": userData,
		"meta-data": metaData,
	}); err != nil {
		return "", nil, fmt.Errorf("can't write user-data: %v", err)
	}

	isoFile, err := ioutil.TempFile("", "nocloud-iso-")
	if err != nil {
		return "", nil, fmt.Errorf("can't create temporary file: %v", err)
	}
	isoFile.Close()

	if err := utils.GenIsoImage(isoFile.Name(), "cidata", tmpDir); err != nil {
		if rmErr := os.Remove(isoFile.Name()); rmErr != nil {
			glog.Warning("Error removing temporary file %s: %v", isoFile.Name(), rmErr)
		}
		return "", nil, fmt.Errorf("error generating iso image: %v", err)
	}

	diskDef := &libvirtxml.DomainDisk{
		Type:     "file",
		Device:   "disk",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: isoFile.Name()},
		Target:   &libvirtxml.DomainDiskTarget{Bus: "virtio"},
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	}
	return isoFile.Name(), diskDef, nil
}
