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

	content := []byte("{\"capacity\": \"424242\",\n\"uuid\":\"123\"}")
	if _, err := tempfile.Write(content); err != nil {
		return "", err
	}
	tempfile.Close()

	return tempfile.Name(), nil
}

func TestQCOW2VolumeNaming(t *testing.T) {
	v := qcow2Volume{
		volumeBase: volumeBase{
			&VMConfig{DomainUUID: testUuid},
			nil,
		},
		name: TestVolumeName,
	}
	expected := "virtlet-" + testUuid + "-" + TestVolumeName
	volumeName := v.volumeName()
	if volumeName != expected {
		t.Errorf("Incorrect root volume image name. Expected %s, received %s", expected, volumeName)
	}
}

func TestQCOW2VolumeLifeCycle(t *testing.T) {
	rec := fake.NewToplevelRecorder()

	volumesPoolPath := "/fake/volumes/pool"
	expectedVolumePath := volumesPoolPath + "/virtlet-" + testUuid + "-" + TestVolumeName
	spool := fake.NewFakeStoragePool(rec.Child("volumes"), "volumes", volumesPoolPath)

	im := NewFakeImageManager(rec.Child("image"))

	optsFilePath, err := prepareOptsFileForQcow2Volume()
	if err != nil {
		t.Fatalf("prepareOptsFileForQcow2Volume returned an error: %v", err)
	}
	defer os.Remove(optsFilePath)

	volume, err := newQCOW2Volume(
		TestVolumeName,
		optsFilePath,
		&VMConfig{DomainUUID: testUuid, Image: "rootfs image name"},
		newFakeVolumeOwner(spool, im),
	)
	if err != nil {
		t.Fatalf("newQCOW2Volume returned an error: %v", err)
	}

	vol, err := volume.Setup()
	if err != nil {
		t.Errorf("Setup returned an error: %v", err)
	}

	if vol.Type != "file" {
		t.Errorf("Expected 'file' volume type, received: %s", vol.Type)
	}

	if vol.Device != "disk" {
		t.Errorf("Expected 'disk' as volume device, received: %s", vol.Device)
	}

	if vol.Source.File != expectedVolumePath {
		t.Errorf("Expected '%s' as volume path, received: %s", expectedVolumePath, vol.Source.File)
	}

	rec.Rec("volume retuned by qcow2_flexvolume", vol)

	if err := volume.Teardown(); err != nil {
		t.Errorf("Teardown returned an error: %v", err)
	}

	gm.Verify(t, rec.Content())
}
