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
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Mirantis/virtlet/tests/gm"
	digest "github.com/opencontainers/go-digest"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	fakeutils "github.com/Mirantis/virtlet/pkg/utils/fake"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

func TestPersistentRootVolume(t *testing.T) {
	fakeImages := []fakeImageSpec{
		{
			name:   "persistent/image1",
			path:   "/fake/path1",
			digest: digest.Digest("sha256:12b05d23a781e4aae1ab9a7de27721cbd1f1d666cfb4e21ab31338eb96eb1e3f"),
			size:   8192,
		},
		{
			name:   "persistent/image2",
			path:   "/fake/path2",
			digest: digest.Digest("sha256:d66ab8e0ea2931d41e27ba4f1d9c007a1d43ab883158a8a22f90872a8d9bb0e3"),
			size:   10000,
		},
	}
	for _, tc := range []struct {
		name              string
		imageName         string
		secondImageName   string
		dmPath            string
		fileSize          uint64
		imageWrittenAgain bool
		useSymlink        bool
		errors            [2]string
	}{
		{
			name:            "image unchanged",
			imageName:       "persistent/image1",
			secondImageName: "persistent/image1",
			fileSize:        8704, // just added a sector
		},
		{
			name:              "image change",
			imageName:         "persistent/image1",
			secondImageName:   "persistent/image2",
			fileSize:          16384,
			imageWrittenAgain: true,
		},
		{
			name:      "first image too big",
			imageName: "persistent/image1",
			fileSize:  4096,
			errors: [2]string{
				"too small",
				"",
			},
		},
		{
			name:            "second image too big",
			imageName:       "persistent/image1",
			secondImageName: "persistent/image2",
			fileSize:        8704,
			errors: [2]string{
				"",
				"too small",
			},
		},
		{
			name:            "symlinks",
			imageName:       "persistent/image1",
			secondImageName: "persistent/image1",
			fileSize:        8704,
			useSymlink:      true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := testutils.NewToplevelRecorder()
			im := newFakeImageManager(rec.Child("image"), fakeImages...)
			if tc.fileSize%512 != 0 {
				t.Fatalf("block device size must be a multiple of 512")
			}

			tmpDir, err := ioutil.TempDir("", "fake-persistent-rootfs")
			if err != nil {
				t.Fatalf("TempDir(): %v", err)
			}
			defer os.RemoveAll(tmpDir)
			fakeDevDir := filepath.Join(tmpDir, "__dev__")
			if err := os.Mkdir(fakeDevDir, 0777); err != nil {
				t.Fatalf("Mkdir(): %v", err)
			}

			devPath := filepath.Join(fakeDevDir, "rootdev")
			devFile, err := os.Create(devPath)
			if err != nil {
				t.Fatalf("Create(): %v", err)
			}
			if _, err := devFile.Write(make([]byte, tc.fileSize)); err != nil {
				devFile.Close()
				t.Fatalf("Write(): %v", err)
			}
			if err := devFile.Close(); err != nil {
				t.Fatalf("devFile.Close()")
			}
			devPathToUse := devPath
			if tc.useSymlink {
				devPathToUse := filepath.Join(fakeDevDir, "rootdevlink")
				if err := os.Symlink(devPath, devPathToUse); err != nil {
					t.Fatalf("Symlink(): %v", err)
				}
			}

			for n, imageName := range []string{tc.imageName, tc.secondImageName} {
				if imageName == "" {
					continue
				}
				cmdSpecs := []fakeutils.CmdSpec{
					{
						Match:  "blockdev --getsz",
						Stdout: strconv.Itoa(int(tc.fileSize / 512)),
					},
					{
						Match: "dmsetup create",
					},
					{
						Match: "dmsetup remove",
					},
				}
				if n == 0 || tc.imageWrittenAgain {
					// qemu-img convert is used to write the image to the block device.
					// It should only be called if the image changes.
					cmdSpecs = append(cmdSpecs, fakeutils.CmdSpec{
						Match: "qemu-img convert",
					})
				}
				cmd := fakeutils.NewCommander(rec, cmdSpecs)
				cmd.ReplaceTempPath("__dev__", "/dev")
				owner := newFakeVolumeOwner(nil, im, cmd)
				rootVol := getPersistentRootVolume(t, imageName, devPathToUse, owner)
				verifyRootVolumeSetup(t, rec, rootVol, tc.errors[n])
				if tc.errors[n] == "" {
					verifyRootVolumeTeardown(t, rec, rootVol)
				}
			}
			gm.Verify(t, gm.NewYamlVerifier(rec.Content()))
		})
	}
}

func verifyRootVolumeSetup(t *testing.T, rec testutils.Recorder, rootVol *persistentRootVolume, expectedError string) {
	rec.Rec("setup", nil)
	vol, fs, err := rootVol.Setup()
	if expectedError == "" {
		if err != nil {
			t.Fatalf("Setup returned an unexpected error: %v", err)
		}
	} else {
		switch {
		case err == nil:
			t.Errorf("Setup didn't return the expected error")
		case !strings.Contains(err.Error(), expectedError):
			t.Errorf("Setup returned a wrong error message %q (must contain %q)", err, expectedError)
		}
		return
	}

	if fs != nil {
		t.Errorf("Didn't expect a filesystem")
	}

	if vol.Source.Block == nil {
		t.Errorf("Expected 'block' volume type")
	}

	if vol.Device != "disk" {
		t.Errorf("Expected 'disk' as volume device, received: %s", vol.Device)
	}

	expectedDmPath := "/dev/mapper/virtlet-dm-" + testUUID
	if vol.Source.Block.Dev != expectedDmPath {
		t.Errorf("Expected '%s' as root block device path, received: %s", expectedDmPath, vol.Source.Block.Dev)
	}

	out, err := vol.Marshal()
	if err != nil {
		t.Fatalf("error marshalling the volume: %v", err)
	}
	rec.Rec("end setup -- root disk", out)
}

func verifyRootVolumeTeardown(t *testing.T, rec testutils.Recorder, rootVol *persistentRootVolume) {
	rec.Rec("teardown", nil)
	if err := rootVol.Teardown(); err != nil {
		t.Fatalf("Teardown(): %v", err)
	}
	rec.Rec("end teardown", nil)
}

func getPersistentRootVolume(t *testing.T, imageName, devHostPath string, owner volumeOwner) *persistentRootVolume {
	volumes, err := GetRootVolume(
		&types.VMConfig{
			PodSandboxID: testUUID,
			Image:        imageName,
			VolumeDevices: []types.VMVolumeDevice{
				{
					DevicePath: "/",
					HostPath:   devHostPath,
				},
			},
		}, owner)
	if err != nil {
		t.Fatalf("GetRootVolume returned an error: %v", err)
	}

	if len(volumes) != 1 {
		t.Fatalf("GetRootVolumes returned non single number of volumes: %d", len(volumes))
	}

	return volumes[0].(*persistentRootVolume)
}
