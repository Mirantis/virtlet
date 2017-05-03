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
	"path/filepath"
)

const (
	noCloudDiskTemplate = `
<disk type="file" device="disk">
  <driver name="qemu" type="raw"/>
  <source file='%s'/>
  <readonly/>
  <target dev="%%s" bus="virtio"/>
</disk>
`
)

func noCloudVolumeHandler(uuidGen UuidGen, targetDir string, opts volumeOpts) (map[string][]byte, error) {
	isoPath := filepath.Join(targetDir, "cidata.iso")
	return map[string][]byte{
		"disk.xml":            []byte(fmt.Sprintf(noCloudDiskTemplate, isoPath)),
		"cidata.cd/meta-data": []byte(opts.MetaData),
		"cidata.cd/user-data": []byte(opts.UserData),
	}, nil
}
