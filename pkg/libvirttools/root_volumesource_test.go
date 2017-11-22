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
	"testing"

	"github.com/Mirantis/virtlet/pkg/virt"
	"github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/gm"
)

const (
	testUuid = "77f29a0e-46af-4188-a6af-9ff8b8a65224"
)

func TestRootVolumeNaming(t *testing.T) {
	v := rootVolume{
		volumeBase{
			&VMConfig{DomainUUID: testUuid},
			nil,
		},
	}
	expected := "virtlet_root_" + testUuid

	cloneName := v.cloneName()

	if cloneName != expected {
		t.Errorf("Incorrect root volume clone name. Expected %s, received %s", expected, cloneName)
	}
}

func TestRootVolumeLifeCycle(t *testing.T) {
	rec := fake.NewToplevelRecorder()

	volumesPoolPath := "/fake/volumes/pool"
	expectedRootVolumePath := volumesPoolPath + "/virtlet_root_" + testUuid
	spool := fake.NewFakeStoragePool(rec.Child("volumes"), "volumes", volumesPoolPath)
	ipool := fake.NewFakeStoragePool(rec.Child("images"), "images", "/fake/images/pool")

	im := fake.NewFakeImageManager(rec.Child("image"), ipool)

	volumes, err := GetRootVolume(
		&VMConfig{DomainUUID: testUuid, Image: "rootfs image name"},
		newFakeVolumeOwner(spool, im),
	)
	if err != nil {
		t.Errorf("GetRootVolume returned an error: %v", err)
	}

	if len(volumes) != 1 {
		t.Errorf("GetRootVolumes returned non single number of volumes: %d", len(volumes))
	}

	rootVol := volumes[0]

	vol, err := rootVol.Setup(nil)
	if err != nil {
		t.Errorf("Setup returned an error: %v", err)
	}

	if vol.Type != "file" {
		t.Errorf("Expected 'file' volume type, received: %s", vol.Type)
	}

	if vol.Device != "disk" {
		t.Errorf("Expected 'disk' as volume device, received: %s", vol.Device)
	}

	if vol.Source.File != expectedRootVolumePath {
		t.Errorf("Expected '%s' as root volume path, received: %s", vol.Source.File)
	}

	rec.Rec("root disk retuned by virtlet_root_volumesource", vol)

	if err := rootVol.Teardown(); err != nil {
		t.Errorf("Teardown returned an error: %v", err)
	}

	gm.Verify(t, rec.Content())
}

type fakeVolumeOwner struct {
	storagePool  *fake.FakeStoragePool
	imageManager *fake.FakeImageManager
}

var _ VolumeOwner = fakeVolumeOwner{}

func newFakeVolumeOwner(storagePool *fake.FakeStoragePool, imageManager *fake.FakeImageManager) *fakeVolumeOwner {
	return &fakeVolumeOwner{
		storagePool:  storagePool,
		imageManager: imageManager,
	}
}

func (vo fakeVolumeOwner) StoragePool() virt.VirtStoragePool {
	return vo.storagePool
}

func (vo fakeVolumeOwner) DomainConnection() virt.VirtDomainConnection {
	return nil
}

func (vo fakeVolumeOwner) ImageManager() ImageManager {
	return vo.imageManager
}

func (vo fakeVolumeOwner) RawDevices() []string { return nil }

func (vo fakeVolumeOwner) KubeletRootDir() string { return "" }
