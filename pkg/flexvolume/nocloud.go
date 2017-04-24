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
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	noCloudIsoName      = "nocloud.iso"
	noCloudDiskTemplate = `
<disk type="file" device="disk">
  <driver name="qemu" type="raw"/>
  <source file='%s'/>
  <readonly/>
  <target dev="%%s" bus="virtio"/>
</disk>
`
	noCloudIsoVolumeId = "cidata"
)

type noCloudVolumeType struct{}

var _ volumeType = noCloudVolumeType{}

func (_ noCloudVolumeType) populateVolumeDir(uuidGen UuidGen, targetDir string, opts volumeOpts) error {
	isoPath := filepath.Join(targetDir, noCloudIsoName)
	if err := genIsoImage(isoPath, map[string][]byte{
		"meta-data": []byte(opts.MetaData),
		"user-data": []byte(opts.UserData),
	}); err != nil {
		return err
	}
	diskXML := fmt.Sprintf(noCloudDiskTemplate, isoPath)
	if err := ioutil.WriteFile(filepath.Join(targetDir, "disk.xml"), []byte(diskXML), 0644); err != nil {
		return fmt.Errorf("error writing disk.xml: %v", err)
	}

	return nil
}

func (_ noCloudVolumeType) getVolumeName(opts volumeOpts) (string, error) {
	h1 := sha1.New()
	io.WriteString(h1, opts.UserData)
	h2 := sha1.New()
	io.WriteString(h2, opts.MetaData)
	return fmt.Sprintf("nocloud/%x-%x", h1.Sum(nil), h2.Sum(nil)), nil
}

func genIsoImage(isoPath string, content map[string][]byte) error {
	tmpDir, err := ioutil.TempDir("", "nocloud-iso")
	if err != nil {
		return fmt.Errorf("error making temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	for filename, bs := range content {
		p := filepath.Join(tmpDir, filename)
		if err := ioutil.WriteFile(p, bs, 0644); err != nil {
			return fmt.Errorf("error writing %q: %v", p, err)
		}
	}

	// genisoimage -o nocloud.iso -V cidata -r -J meta-data user-data
	out, err := exec.Command("genisoimage", "-o", isoPath, "-V", noCloudIsoVolumeId, "-r", "-J", tmpDir).CombinedOutput()
	if err != nil {
		outStr := ""
		if len(out) != 0 {
			outStr = ". Output:\n" + string(out)
		}
		return fmt.Errorf("error generating iso: %v%s", err, outStr)
	}

	return nil
}
