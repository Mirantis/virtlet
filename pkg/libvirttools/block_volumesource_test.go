/*
Copyright 2018 Mirantis

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
	"encoding/xml"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

func TestBlockVolumeSource(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "block-vol-test-")
	if err != nil {
		t.Fatalf("TempDir() returned: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// On Mac, /tmp is a symlink and it may make this test fail
	// unless we eval it
	baseDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	devs := []string{
		filepath.Join(baseDir, "test1"),
		filepath.Join(baseDir, "test2"),
	}
	for _, dev := range devs {
		if f, err := os.OpenFile(dev, os.O_RDONLY|os.O_CREATE, 0666); err != nil {
			t.Fatal(err)
		} else {
			f.Close()
		}
	}
	symlinkName := filepath.Join(baseDir, "test2link")
	if err := os.Symlink(devs[1], symlinkName); err != nil {
		t.Fatal(err)
	}

	volumes, err := GetBlockVolumes(&types.VMConfig{
		VolumeDevices: []types.VMVolumeDevice{
			{
				DevicePath: "/dev/test1",
				HostPath:   devs[0],
			},
			{
				DevicePath: "/dev/test1",
				HostPath:   symlinkName,
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("GetRootVolume returned an error: %v", err)
	}

	expectedDisks := []*libvirtxml.DomainDisk{
		{
			Device: "disk",
			Source: &libvirtxml.DomainDiskSource{Block: &libvirtxml.DomainDiskSourceBlock{Dev: devs[0]}},
			Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		},
		{
			Device: "disk",
			Source: &libvirtxml.DomainDiskSource{Block: &libvirtxml.DomainDiskSourceBlock{Dev: devs[1]}},
			Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		},
	}
	var disks []*libvirtxml.DomainDisk
	for _, vol := range volumes {
		if disk, _, err := vol.Setup(); err != nil {
			t.Fatal(err)
		} else {
			disks = append(disks, disk)
		}
	}
	if !reflect.DeepEqual(expectedDisks, disks) {
		expected, err := xml.MarshalIndent(expectedDisks, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := xml.MarshalIndent(disks, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		t.Errorf("Bad disk defs generated. Expected:\n%s\nGot:\n%s", expected, actual)
	}
	// TODO: symlink
}
