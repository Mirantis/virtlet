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

func cephVolumeHandler(uuidGen UuidGen, targetDir string, opts volumeOpts) (map[string][]byte, error) {
	uuid := uuidGen()
	pairIPPort := strings.Split(opts.Monitor, ":")
	if len(pairIPPort) != 2 {
		return nil, fmt.Errorf("invalid format of ceph monitor setting: %s. Expected in form ip:port", opts.Monitor)
	}
	return map[string][]byte{
		// Note: target dev name will be specified by virtlet later when building full domain xml definition
		"disk.xml":   []byte(fmt.Sprintf(cephDiskTemplate, opts.User, uuid, opts.Pool, opts.Volume, pairIPPort[0], pairIPPort[1])),
		"secret.xml": []byte(fmt.Sprintf(secretTemplate, uuid, opts.User)),
		// Will be removed right after creating appropriate secret in libvirt
		"key": []byte(opts.Secret),
	}, nil
}
