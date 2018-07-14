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
	"io/ioutil"
	"os"
	"testing"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/gm"
)

const (
	TestVolumeName = "test-volume"
)

func prepareOptsFileForQcow2Volume() (string, error) {
	tempfile, err := ioutil.TempFile("", "qcow2-flexvol-test-")
	if err != nil {
		return "", err
	}
	defer tempfile.Close()

	content := []byte("{\"capacity\": \"424242\",\n\"uuid\":\"123\"}")
	if _, err := tempfile.Write(content); err != nil {
		return "", err
	}

	return tempfile.Name(), nil
}

func TestQCOW2VolumeNaming(t *testing.T) {
	v := qcow2Volume{
		volumeBase: volumeBase{
			&types.VMConfig{DomainUUID: testUUID},
			nil,
		},
		name: TestVolumeName,
	}
	expected := "virtlet-" + testUUID + "-" + TestVolumeName
	volumeName := v.volumeName()
	if volumeName != expected {
		t.Errorf("Incorrect root volume image name. Expected %s, received %s", expected, volumeName)
	}
}

func TestQCOW2VolumeLifeCycle(t *testing.T) {
	rec := testutils.NewToplevelRecorder()

	volumesPoolPath := "/fake/volumes/pool"
	expectedVolumePath := volumesPoolPath + "/virtlet-" + testUUID + "-" + TestVolumeName
	spool := fake.NewFakeStoragePool(rec.Child("volumes"), &libvirtxml.StoragePool{
		Name:   "volumes",
		Target: &libvirtxml.StoragePoolTarget{Path: volumesPoolPath},
	})

	im := NewFakeImageManager(rec.Child("image"))

	optsFilePath, err := prepareOptsFileForQcow2Volume()
	if err != nil {
		t.Fatalf("prepareOptsFileForQcow2Volume returned an error: %v", err)
	}
	defer os.Remove(optsFilePath)

	volume, err := newQCOW2Volume(
		TestVolumeName,
		optsFilePath,
		&types.VMConfig{DomainUUID: testUUID, Image: "rootfs image name"},
		newFakeVolumeOwner(spool, im),
	)
	if err != nil {
		t.Fatalf("newQCOW2Volume returned an error: %v", err)
	}

	vol, _, err := volume.Setup()
	if err != nil {
		t.Errorf("Setup returned an error: %v", err)
	}

	if vol.Source.File == nil {
		t.Errorf("Expected 'file' volume type")
	}

	if vol.Device != "disk" {
		t.Errorf("Expected 'disk' as volume device, received: %s", vol.Device)
	}

	if vol.Source.File.File != expectedVolumePath {
		t.Errorf("Expected '%s' as volume path, received: %s", expectedVolumePath, vol.Source.File.File)
	}

	out, err := vol.Marshal()
	if err != nil {
		t.Fatalf("error marshalling the volume: %v", err)
	}
	rec.Rec("volume retuned by qcow2_flexvolume", out)

	if err := volume.Teardown(); err != nil {
		t.Errorf("Teardown returned an error: %v", err)
	}

	gm.Verify(t, gm.NewYamlVerifier(rec.Content()))
}
