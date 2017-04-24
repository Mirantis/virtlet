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
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
)

const (
	secretTemplate = `
<secret ephemeral='no' private='no'>
  <uuid>%s</uuid>
  <usage type='ceph'>
    <name>%s</name>
  </usage>
</secret>
`
	cephDiskTemplate = `
<disk type="network" device="disk">
  <driver name="qemu" type="raw"/>
  <auth username="%s">
    <secret type="ceph" uuid="%s"/>
  </auth>
  <source protocol="rbd" name="%s/%s">
    <host name="%s" port="%s"/>
  </source>
  <target dev="%%s" bus="virtio"/>
</disk>
`
)

type cephVolumeType struct{}

var _ volumeType = cephVolumeType{}

func (_ cephVolumeType) populateVolumeDir(uuidGen UuidGen, targetDir string, opts volumeOpts) error {
	uuid := uuidGen()

	secretXML := fmt.Sprintf(secretTemplate, uuid, opts.User)
	if err := ioutil.WriteFile(filepath.Join(targetDir, "secret.xml"), []byte(secretXML), 0644); err != nil {
		return fmt.Errorf("error writing secret.xml: %v", err)
	}

	// Will be removed right after creating appropriate secret in libvirt
	if err := ioutil.WriteFile(filepath.Join(targetDir, "/key"), []byte(opts.Secret), 0644); err != nil {
		return fmt.Errorf("error writing ceph key: %v", err)
	}

	pairIPPort := strings.Split(opts.Monitor, ":")
	if len(pairIPPort) != 2 {
		return fmt.Errorf("invalid format of ceph monitor setting: %s. Expected in form ip:port", opts.Monitor)
	}
	diskXML := fmt.Sprintf(cephDiskTemplate, opts.User, uuid, opts.Pool, opts.Volume, pairIPPort[0], pairIPPort[1])
	// Note: target dev name will be specified by virtlet later when building full domain xml definition
	if err := ioutil.WriteFile(filepath.Join(targetDir, "disk.xml"), []byte(diskXML), 0644); err != nil {
		return fmt.Errorf("error writing disk.xml: %v", err)
	}
	return nil
}

func (_ cephVolumeType) getVolumeName(opts volumeOpts) (string, error) {
	r := []string{"ceph"}
	monStr := strings.Replace(opts.Monitor, ":", "/", -1)
	for _, s := range []string{monStr, opts.Pool, opts.Volume, opts.User, opts.Protocol} {
		if s != "" {
			r = append(r, s)
		}
	}
	if len(r) == 0 {
		return "", fmt.Errorf("invalid flexvolume definition")
	}
	return strings.Join(r, "/"), nil
}
