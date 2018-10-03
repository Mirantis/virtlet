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

package fake

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

const dmsetupTable = `vg1-home_rimage_1: 0 209715200 linear 8:1 10240
vg1-home_rimage_0: 0 209715200 linear 8:17 29304832
vg1-home: 0 209715200 raid raid1 3 0 region_size 1024 2 253:1 253:2 253:3 253:4
virtlet-dm-5edfe2ad-9852-439b-bbfb-3fe8b7c72906: 0 8191999 linear 252:0 1
virtlet-dm-92ed2bdc-4b47-43ce-b0ba-ff1c06a2652d: 0 8191999 linear 252:1 1
virtlet-dm-9a322047-1f0d-4395-8e43-6e1b310ce6f3: 0 8191999 linear 252:2 1
vg1-home_rmeta_1: 0 8192 linear 8:1 2048
vg1-home_rmeta_0: 0 8192 linear 8:17 29296640
vg1-swap: 0 29294592 linear 8:1 908404736
vg1-root: 0 29294592 linear 8:17 2048
vg1-var: 0 1397358592 striped 2 128 8:1 209725440 8:17 239020032
`

var ueventFiles = map[string]string{
	"252:0": `MAJOR=252
MINOR=0
DEVNAME=rootdev
DEVTYPE=disk
`,
	"252:1": `MAJOR=252
MINOR=0
DEVNAME=nonrootdev
DEVTYPE=disk
`,
	"252:2": `MAJOR=252
MINOR=0
DEVNAME=rootdev1
DEVTYPE=disk
`,
	"8:1": `MAJOR=8
MINOR=1
DEVNAME=swapdev
DEVTYPE=disk
`,
}

// WithFakeRootDevs calls the specified function passing it the paths to
// the fake block devices and their containing directory.
func WithFakeRootDevs(t *testing.T, size uint64, names []string, toCall func(devPaths []string, devDir string)) {
	tmpDir, err := ioutil.TempDir("", "fake-blockdev")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)
	fakeDevDir := filepath.Join(tmpDir, "__dev__")
	if err := os.Mkdir(fakeDevDir, 0777); err != nil {
		t.Fatalf("Mkdir(): %v", err)
	}

	var devPaths []string
	for _, name := range names {
		devPath := filepath.Join(fakeDevDir, name)
		if err := ioutil.WriteFile(devPath, make([]byte, size), 0666); err != nil {
			t.Fatalf("WriteFile(): %v", err)
		}
		devPaths = append(devPaths, devPath)

	}
	toCall(devPaths, fakeDevDir)
}

// WithFakeRootDev calls the specified function passing it the path to
// the fake block device and its containing directory.
func WithFakeRootDev(t *testing.T, size uint64, toCall func(devPath, devDir string)) {
	WithFakeRootDevs(t, size, []string{"rootdev"}, func(devPaths []string, devDir string) {
		toCall(devPaths[0], devDir)
	})
}

// WithFakeRootDevsAndSysfs calls the specified function passing it
// the paths to the fake block devices and their containing directory,
// as well as the path to fake sysfs containing uevent entries for the
// fake devices.
func WithFakeRootDevsAndSysfs(t *testing.T, toCall func(devPaths []string, table, devDir, sysfsDir string)) {
	WithFakeRootDevs(t, 2048, []string{"rootdev", "rootdev1"}, func(devPaths []string, devDir string) {
		// write dummy headerless file as swapdev and nonrootdev
		for _, devName := range []string{"swapdev", "nonrootdev"} {
			if err := ioutil.WriteFile(filepath.Join(devDir, devName), make([]byte, 1024), 0666); err != nil {
				t.Fatalf("WriteFile(): %v", err)
			}
		}

		sysfsDir := filepath.Join(devDir, "sys")
		for id, content := range ueventFiles {
			devInfoDir := filepath.Join(sysfsDir, "dev/block", id)
			if err := os.MkdirAll(devInfoDir, 0777); err != nil {
				t.Fatalf("MkdirAll(): %v", err)
			}
			if err := ioutil.WriteFile(filepath.Join(devInfoDir, "uevent"), []byte(content), 0666); err != nil {
				t.Fatalf("WriteFile(): %v", err)
			}
		}

		toCall(devPaths, dmsetupTable, devDir, sysfsDir)
	})
}
