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
	"fmt"
	"testing"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
	digest "github.com/opencontainers/go-digest"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/utils"
	fakeutils "github.com/Mirantis/virtlet/pkg/utils/fake"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/pkg/virt"
	"github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/gm"
)

const (
	testUUID                 = "77f29a0e-46af-4188-a6af-9ff8b8a65224"
	fakeImageVirtualSize     = 424242
	fakeImageStoreUsedBytes  = 424242
	fakeImageStoreUsedInodes = 424242
	fakeImageDigest          = digest.Digest("sha256:c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2")
)

type fakeImageSpec struct {
	name   string
	path   string
	digest digest.Digest
	size   uint64
}

type fakeImageManager struct {
	rec      testutils.Recorder
	imageMap map[string]fakeImageSpec
}

var _ ImageManager = &fakeImageManager{}

func newFakeImageManager(rec testutils.Recorder, extraImages ...fakeImageSpec) *fakeImageManager {
	m := make(map[string]fakeImageSpec)
	for _, img := range append(extraImages, fakeImageSpec{
		name:   "fake/image1",
		path:   "/fake/volume/path",
		digest: fakeImageDigest,
		size:   fakeImageVirtualSize,
	}) {
		m[img.name] = img
	}
	return &fakeImageManager{
		rec:      rec,
		imageMap: m,
	}
}

func (im *fakeImageManager) GetImagePathDigestAndVirtualSize(imageName string) (string, digest.Digest, uint64, error) {
	im.rec.Rec("GetImagePathDigestAndVirtualSize", imageName)
	spec, found := im.imageMap[imageName]
	if !found {
		return "", "", 0, fmt.Errorf("image %q not found", imageName)
	}
	return spec.path, spec.digest, spec.size, nil
}

func (im *fakeImageManager) FilesystemStats() (*types.FilesystemStats, error) {
	return &types.FilesystemStats{
		Mountpoint: "/some/dir",
		UsedBytes:  fakeImageStoreUsedBytes,
		UsedInodes: fakeImageStoreUsedInodes,
	}, nil
}

func (im *fakeImageManager) BytesUsedBy(path string) (uint64, error) {
	im.rec.Rec("BytesUsedBy", path)
	return fakeImageVirtualSize, nil
}

func TestRootVolumeNaming(t *testing.T) {
	v := rootVolume{
		volumeBase{
			&types.VMConfig{DomainUUID: testUUID},
			nil,
		},
	}
	expected := "virtlet_root_" + testUUID
	volumeName := v.volumeName()
	if volumeName != expected {
		t.Errorf("Incorrect root volume image name. Expected %s, received %s", expected, volumeName)
	}
}

func getRootVolumeForTest(t *testing.T, vmConfig *types.VMConfig) (*rootVolume, *testutils.TopLevelRecorder, *fake.FakeStoragePool) {
	rec := testutils.NewToplevelRecorder()
	volumesPoolPath := "/fake/volumes/pool"
	spool := fake.NewFakeStoragePool(rec.Child("volumes"), &libvirtxml.StoragePool{
		Name:   "volumes",
		Target: &libvirtxml.StoragePoolTarget{Path: volumesPoolPath},
	})
	im := newFakeImageManager(rec.Child("image"))

	volumes, err := GetRootVolume(
		vmConfig,
		newFakeVolumeOwner(spool, im, fakeutils.NewCommander(nil, nil)))
	if err != nil {
		t.Fatalf("GetRootVolume returned an error: %v", err)
	}

	if len(volumes) != 1 {
		t.Fatalf("GetRootVolumes returned non single number of volumes: %d", len(volumes))
	}

	return volumes[0].(*rootVolume), rec, spool
}

func TestRootVolumeSize(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		specifiedRootVolumeSize int64
		expectedVolumeSize      int64
	}{
		{
			name: "default (zero)",
			specifiedRootVolumeSize: 0,
			expectedVolumeSize:      fakeImageVirtualSize,
		},
		{
			name: "negative",
			specifiedRootVolumeSize: -1,
			expectedVolumeSize:      fakeImageVirtualSize,
		},
		{
			name: "smaller than fakeImageVirtualSize",
			specifiedRootVolumeSize: fakeImageVirtualSize - 10,
			expectedVolumeSize:      fakeImageVirtualSize,
		},
		{
			name: "same as fakeImageVirtualSize",
			specifiedRootVolumeSize: fakeImageVirtualSize,
			expectedVolumeSize:      fakeImageVirtualSize,
		},
		{
			name: "greater than fakeImageVirtualSize",
			specifiedRootVolumeSize: fakeImageVirtualSize + 10,
			expectedVolumeSize:      fakeImageVirtualSize + 10,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rootVol, rec, spool := getRootVolumeForTest(t, &types.VMConfig{
				DomainUUID: testUUID,
				Image:      "fake/image1",
				ParsedAnnotations: &types.VirtletAnnotations{
					RootVolumeSize: tc.specifiedRootVolumeSize,
				},
			})

			_, _, err := rootVol.Setup()
			if err != nil {
				t.Fatalf("Setup returned an error: %v", err)
			}

			virtVol, err := spool.LookupVolumeByName(rootVol.volumeName())
			if err != nil {
				t.Fatalf("couldn't find volume %q", rootVol.volumeName())
			}

			size, err := virtVol.Size()
			if err != nil {
				t.Fatalf("couldn't get virt volume size: %v", err)
			}

			if int64(size) != tc.expectedVolumeSize {
				t.Errorf("bad volume size %d instead of %d", size, tc.expectedVolumeSize)
			}
			gm.Verify(t, gm.NewYamlVerifier(rec.Content()))
		})
	}
}

func TestRootVolumeLifeCycle(t *testing.T) {
	expectedRootVolumePath := "/fake/volumes/pool/virtlet_root_" + testUUID
	rootVol, rec, _ := getRootVolumeForTest(t, &types.VMConfig{
		DomainUUID: testUUID,
		Image:      "fake/image1",
	})

	vol, fs, err := rootVol.Setup()
	if err != nil {
		t.Fatalf("Setup returned an error: %v", err)
	}

	if fs != nil {
		t.Errorf("Didn't expect a filesystem")
	}

	if vol.Source.File == nil {
		t.Errorf("Expected 'file' volume type")
	}

	if vol.Device != "disk" {
		t.Errorf("Expected 'disk' as volume device, received: %s", vol.Device)
	}

	if vol.Source.File.File != expectedRootVolumePath {
		t.Errorf("Expected '%s' as root volume path, received: %s", expectedRootVolumePath, vol.Source.File)
	}

	out, err := vol.Marshal()
	if err != nil {
		t.Fatalf("error marshalling the volume: %v", err)
	}
	rec.Rec("root disk retuned by virtlet_root_volumesource", out)

	if err := rootVol.Teardown(); err != nil {
		t.Errorf("Teardown returned an error: %v", err)
	}

	gm.Verify(t, gm.NewYamlVerifier(rec.Content()))
}

type fakeVolumeOwner struct {
	storagePool  *fake.FakeStoragePool
	imageManager *fakeImageManager
	commander    *fakeutils.FakeCommander
}

var _ volumeOwner = fakeVolumeOwner{}

func newFakeVolumeOwner(storagePool *fake.FakeStoragePool, imageManager *fakeImageManager, commander *fakeutils.FakeCommander) *fakeVolumeOwner {
	return &fakeVolumeOwner{
		storagePool:  storagePool,
		imageManager: imageManager,
		commander:    commander,
	}
}

func (vo fakeVolumeOwner) StoragePool() (virt.StoragePool, error) {
	return vo.storagePool, nil
}

func (vo fakeVolumeOwner) DomainConnection() virt.DomainConnection {
	return nil
}

func (vo fakeVolumeOwner) ImageManager() ImageManager {
	return vo.imageManager
}

func (vo fakeVolumeOwner) RawDevices() []string { return nil }

func (vo fakeVolumeOwner) KubeletRootDir() string { return "" }

func (vo fakeVolumeOwner) VolumePoolName() string { return "" }

func (vo fakeVolumeOwner) Mounter() utils.Mounter { return utils.NullMounter }

func (vo fakeVolumeOwner) SharedFilesystemPath() string { return "/var/lib/virtlet/fs" }

func (vo fakeVolumeOwner) Commander() utils.Commander { return vo.commander }
