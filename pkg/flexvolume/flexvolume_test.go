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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

const (
	fakeUuid = "abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"
)

type fakeMounter struct {
	t       *testing.T
	tmpDir  string
	journal []string
}

var _ utils.Mounter = &fakeMounter{}

func newFakeMounter(t *testing.T, tmpDir string) *fakeMounter {
	return &fakeMounter{t: t, tmpDir: tmpDir}
}

func (mounter *fakeMounter) validatePath(target string) {
	if filepath.Dir(target) != filepath.Clean(mounter.tmpDir) {
		mounter.t.Fatalf("bad path encountered by the mounter: %q (tmpDir %q)", target, mounter.tmpDir)
	}
}

func (mounter *fakeMounter) Mount(source string, target string, fstype string) error {
	mounter.validatePath(target)
	mounter.journal = append(mounter.journal, fmt.Sprintf("mount: %s %s %s", source, target, fstype))

	// We want to check directory contents both before & after mount,
	// see comment in FlexVolumeDriver.mount() in flexvolume.go.
	// So we move the original contents to .shadowed subdir.
	shadowedPath := filepath.Join(target, ".shadowed")
	if err := os.Mkdir(shadowedPath, 0755); err != nil {
		mounter.t.Fatalf("os.Mkdir(): %v", err)
	}

	pathsToShadow, err := filepath.Glob(filepath.Join(target, "*"))
	if err != nil {
		mounter.t.Fatalf("filepath.Glob(): %v", err)
	}
	for _, pathToShadow := range pathsToShadow {
		filename := filepath.Base(pathToShadow)
		if filename == ".shadowed" {
			continue
		}
		if err := os.Rename(pathToShadow, filepath.Join(shadowedPath, filename)); err != nil {
			mounter.t.Fatalf("os.Rename(): %v", err)
		}
	}
	return nil
}

func (mounter *fakeMounter) Unmount(target string) error {
	// we make sure that path is under our tmpdir before wiping it
	mounter.validatePath(target)
	mounter.journal = append(mounter.journal, fmt.Sprintf("unmount: %s", target))

	paths, err := filepath.Glob(filepath.Join(target, "*"))
	if err != nil {
		mounter.t.Fatalf("filepath.Glob(): %v", err)
	}
	for _, path := range paths {
		if filepath.Base(path) != ".shadowed" {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			mounter.t.Fatalf("os.RemoveAll(): %v", err)
		}
	}

	// We don't clean up '.shadowed' dir here because flexvolume driver
	// recursively removes the whole dir tree anyway.
	return nil
}

func TestFlexVolume(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "flexvolume-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cephJSONOpts := map[string]interface{}{
		"type":    "ceph",
		"monitor": "127.0.0.1:6789",
		"pool":    "libvirt-pool",
		"volume":  "rbd-test-image",
		"secret":  "foobar",
		"user":    "libvirt",
	}
	cephJSONVolumeInfo := map[string]interface{}{
		"uuid": fakeUuid,
	}
	for k, v := range cephJSONOpts {
		cephJSONVolumeInfo[k] = v
	}
	cephDir := path.Join(tmpDir, "ceph")
	for _, step := range []struct {
		name         string
		args         []string
		subdir       string
		status       string
		message      string
		fields       map[string]interface{}
		files        map[string]interface{}
		mountJournal []string
	}{
		{
			name:   "init",
			args:   []string{"init"},
			status: "Success",
		},
		{
			name: "attach",
			args: []string{
				"attach",
				"{}", // not actually used by our impl
				"node1",
			},
			status: "Success",
		},
		{
			name: "isattached",
			args: []string{
				"isattached",
				"{}", // not actually used by our impl
				"node1",
			},
			status: "Success",
			fields: map[string]interface{}{
				"attached": true,
			},
		},
		{
			name: "waitforattach",
			args: []string{
				"waitforattach",
				"/dev/dummydev",
				"{}", // not actually used by our impl
			},
			status: "Success",
			fields: map[string]interface{}{
				"device": "/dev/dummydev",
			},
		},
		{
			name:   "mount-ceph",
			args:   []string{"mount", cephDir, utils.MapToJSON(cephJSONOpts)},
			status: "Success",
			subdir: "ceph",
			files: map[string]interface{}{
				"virtlet-flexvolume.json": utils.MapToJSONUnindented(cephJSONVolumeInfo),
				".shadowed": map[string]interface{}{
					"virtlet-flexvolume.json": utils.MapToJSONUnindented(cephJSONVolumeInfo),
				},
			},
			mountJournal: []string{
				fmt.Sprintf("mount: tmpfs %s tmpfs", cephDir),
			},
		},
		{
			name:   "unmount-ceph",
			args:   []string{"unmount", cephDir},
			status: "Success",
			subdir: "ceph",
			mountJournal: []string{
				fmt.Sprintf("unmount: %s", cephDir),
			},
		},
		{
			name:   "mount-ceph-1",
			args:   []string{"mount", cephDir, utils.MapToJSON(cephJSONOpts)},
			status: "Success",
			subdir: "ceph",
			files: map[string]interface{}{
				"virtlet-flexvolume.json": utils.MapToJSONUnindented(cephJSONVolumeInfo),
				".shadowed": map[string]interface{}{
					"virtlet-flexvolume.json": utils.MapToJSONUnindented(cephJSONVolumeInfo),
				},
			},
			mountJournal: []string{
				fmt.Sprintf("mount: tmpfs %s tmpfs", cephDir),
			},
		},
		{
			name:   "unmount-ceph-1",
			args:   []string{"unmount", cephDir},
			status: "Success",
			subdir: "ceph",
			mountJournal: []string{
				fmt.Sprintf("unmount: %s", cephDir),
			},
		},
		{
			name: "detach",
			args: []string{
				"detach",
				"somedev",
				"node1",
			},
			status: "Success",
		},
		{
			name: "badop",
			args: []string{
				"badop",
			},
			status: "Not supported",
		},
		{
			name: "badmount",
			args: []string{
				"mount",
			},
			status:  "Failure",
			message: "unexpected number of args",
		},
		{
			name:    "noargs",
			args:    []string{},
			status:  "Failure",
			message: "no arguments passed",
		},
	} {
		t.Run(step.name, func(t *testing.T) {
			var subdir string
			args := step.args
			mounter := newFakeMounter(t, tmpDir)
			d := NewFlexVolumeDriver(func() string {
				return fakeUuid
			}, mounter)
			result := d.Run(args)
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(result), &m); err != nil {
				t.Fatalf("failed to unmarshal test result: %v", err)
			}

			msg := ""
			if msgValue, ok := m["message"]; ok {
				msg = msgValue.(string)
			}
			status := m["status"].(string)
			if status != step.status {
				t.Errorf("bad status %q instead of %q", status, step.status)
				if status == "Failure" {
					t.Errorf("failure reported: %q", msg)
				}
			}

			if step.message != "" {
				if !strings.Contains(msg, step.message) {
					t.Errorf("bad message %q (doesn't contain %q)", msg, step.message)
				}
			}
			if step.fields != nil {
				for k, v := range step.fields {
					if !reflect.DeepEqual(m[k], v) {
						t.Errorf("unexpected field value: %q must be '%v' but is '%v'", k, v, m[k])
					}
				}
			}
			if step.subdir != "" {
				files, err := testutils.DirToMap(path.Join(tmpDir, step.subdir))
				if err != nil {
					t.Fatalf("dirToMap() on %q: %v", subdir, err)
				}
				if !reflect.DeepEqual(files, step.files) {
					t.Errorf("bad file content.\n%s\n-- instead of --\n%s", utils.MapToJSON(files), utils.MapToJSON(step.files))
				}
			}
			if !reflect.DeepEqual(mounter.journal, step.mountJournal) {
				t.Errorf("unexpected mount journal: %#v instead of %#v", mounter.journal, step.mountJournal)
			}
		})
	}
}

// TODO: escape xml in iso path
