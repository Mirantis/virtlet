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

package blockdev

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	fake "github.com/Mirantis/virtlet/pkg/blockdev/fake"
	"github.com/Mirantis/virtlet/pkg/utils"
	fakeutils "github.com/Mirantis/virtlet/pkg/utils/fake"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

func TestDevHeader(t *testing.T) {
	for _, tc := range []struct {
		name              string
		content           []string
		dmPath            string
		fileSize          uint64
		imageWrittenAgain bool
		errors            [2]string
	}{
		{
			name:     "image unchanged",
			content:  []string{"image1", "image1"},
			fileSize: 8704, // just added a sector
		},
		{
			name:              "image change",
			content:           []string{"image1", "image2"},
			fileSize:          16384,
			imageWrittenAgain: true,
		},
		{
			name:     "first image too big",
			content:  []string{"image1"},
			fileSize: 4096,
			errors: [2]string{
				"too small",
				"",
			},
		},
		{
			name:     "second image too big",
			content:  []string{"image1", "image2"},
			fileSize: 8704,
			errors: [2]string{
				"",
				"too small",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake.WithFakeRootDev(t, tc.fileSize, func(devPath, devDir string) {
				for n, content := range tc.content {
					if content == "" {
						continue
					}
					cmd := fakeutils.NewCommander(nil, nil)

					ldh := NewLogicalDeviceHandler(cmd, "", "")
					headerExpectedToMatch := n > 0 && tc.content[n-1] == content
					imageHash := sha256.Sum256([]byte(content))
					headerMatches, err := ldh.EnsureDevHeaderMatches(devPath, imageHash)
					if err != nil {
						t.Fatalf("EnsureDevHeaderMatches: %v", err)
					}

					switch {
					case headerMatches == headerExpectedToMatch:
						// ok
					case headerMatches:
						t.Errorf("[%d] the header is expected to match but didn't", n)
					case !headerMatches:
						t.Errorf("[%d] the header is not expected to match but did", n)
					}
				}
			})
		})
	}
}

func TestCreateRemoveVirtualBlockDevice(t *testing.T) {
	fake.WithFakeRootDev(t, 0, func(devPath, devDir string) {
		rec := testutils.NewToplevelRecorder()
		cmd := fakeutils.NewCommander(rec, []fakeutils.CmdSpec{
			{
				Match:  "blockdev --getsz",
				Stdout: "4",
			},
			{
				Match: "dmsetup create",
			},
			{
				Match: "dmsetup remove",
			},
		})
		cmd.ReplaceTempPath("__dev__", "/dev")
		symlinkPath := filepath.Join(devDir, "rootdevlink")
		if err := os.Symlink(devPath, symlinkPath); err != nil {
			t.Fatalf("Symlink(): %v", err)
		}

		ldh := NewLogicalDeviceHandler(cmd, "", "")
		if err := ldh.Map(symlinkPath, "virtlet-dm-foobar", 1024); err != nil {
			t.Fatalf("Map(): %v", err)
		}
		if err := ldh.Unmap("virtlet-dm-foobar"); err != nil {
			t.Fatalf("Unmap(): %v", err)
		}

		expectedRecs := []*testutils.Record{
			{
				Name: "CMD",
				Value: map[string]string{
					"cmd":    "blockdev --getsz /dev/rootdevlink",
					"stdout": "4",
				},
			},
			{
				Name: "CMD",
				Value: map[string]string{
					"cmd":   "dmsetup create virtlet-dm-foobar",
					"stdin": "0 3 linear /dev/rootdev 1\n",
				},
			},
			{
				Name: "CMD",
				Value: map[string]string{
					"cmd": "dmsetup remove virtlet-dm-foobar",
				},
			},
		}
		if !reflect.DeepEqual(expectedRecs, rec.Content()) {
			t.Errorf("bad commands recorded:\n%s\ninstead of\n%s", utils.ToJSON(rec.Content()), utils.ToJSON(expectedRecs))
		}
	})
}

func TestIsVirtletBlockDevice(t *testing.T) {
	fake.WithFakeRootDevsAndSysfs(t, func(devPaths []string, table, devDir, sysfsDir string) {
		cmd := fakeutils.NewCommander(nil, []fakeutils.CmdSpec{
			{
				Match:  "^dmsetup table$",
				Stdout: table,
			},
		})
		ldh := NewLogicalDeviceHandler(cmd, devDir, sysfsDir)
		for _, devPath := range devPaths {
			if _, err := ldh.EnsureDevHeaderMatches(devPath, sha256.Sum256([]byte("foobar"))); err != nil {
				t.Fatalf("EnsureDevHeaderMatches(): %v", err)
			}
		}

		devs, err := ldh.ListVirtletLogicalDevices()
		if err != nil {
			t.Fatalf("ListVirtletLogicalDevices(): %v", err)
		}

		expectedDevs := []string{
			"virtlet-dm-5edfe2ad-9852-439b-bbfb-3fe8b7c72906",
			"virtlet-dm-9a322047-1f0d-4395-8e43-6e1b310ce6f3",
		}
		if !reflect.DeepEqual(devs, expectedDevs) {
			t.Errorf("bad Virtlet block device list: %s instead of %s", utils.ToJSONUnindented(devs), utils.ToJSONUnindented(expectedDevs))
		}
	})
}
