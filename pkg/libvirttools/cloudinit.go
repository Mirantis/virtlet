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
	"path/filepath"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/utils"
)

type CloudInitGenerator struct {
	podName     string
	podNs       string
	annotations *VirtletAnnotations
}

func NewCloudInitGenerator(podName, podNs string, annotations *VirtletAnnotations) *CloudInitGenerator {
	return &CloudInitGenerator{
		podName:     podName,
		podNs:       podNs,
		annotations: annotations,
	}
}

func (g *CloudInitGenerator) generateMetaData() (string, error) {
	m := map[string]interface{}{
		"instance-id":    fmt.Sprintf("%s.%s", g.podName, g.podNs),
		"local-hostname": g.podName,
	}
	if len(g.annotations.SSHKeys) != 0 {
		var keys []string
		for _, key := range g.annotations.SSHKeys {
			keys = append(keys, key)
		}
		m["public-keys"] = keys
	}
	for k, v := range g.annotations.MetaData {
		m[k] = v
	}
	r, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("error marshaling meta-data: %v", err)
	}
	return string(r), nil
}

func (g *CloudInitGenerator) generateUserData() (string, error) {
	if g.annotations.UserDataScript != "" {
		return g.annotations.UserDataScript, nil
	}
	r := []byte{}
	if len(g.annotations.UserData) != 0 {
		var err error
		r, err = yaml.Marshal(g.annotations.UserData)
		if err != nil {
			return "", fmt.Errorf("error marshalling user-data: %v", err)
		}
	}
	return "#cloud-config\n" + string(r), nil
}

func (g *CloudInitGenerator) GenerateDisk() (string, *libvirtxml.DomainDisk, error) {
	tmpDir, err := ioutil.TempDir("", "nocloud-")
	if err != nil {
		return "", nil, fmt.Errorf("can't create temp dir for nocloud: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	metaDataStr, err := g.generateMetaData()
	if err != nil {
		return "", nil, err
	}
	if err := ioutil.WriteFile(filepath.Join(tmpDir, "meta-data"), []byte(metaDataStr), 0777); err != nil {
		return "", nil, fmt.Errorf("can't write meta-data: %v", err)
	}

	userDataStr, err := g.generateUserData()
	if err != nil {
		return "", nil, err
	}
	if err := ioutil.WriteFile(filepath.Join(tmpDir, "user-data"), []byte(userDataStr), 0777); err != nil {
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
