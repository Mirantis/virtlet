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
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	fakefs "github.com/Mirantis/virtlet/pkg/fs/fake"
	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

const (
	fakeUUID = "abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"
)

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
		"uuid": fakeUUID,
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
		mountJournal []*testutils.Record
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
			args:   []string{"mount", cephDir, utils.ToJSON(cephJSONOpts)},
			status: "Success",
			subdir: "ceph",
			files: map[string]interface{}{
				"virtlet-flexvolume.json": utils.ToJSONUnindented(cephJSONVolumeInfo),
				".shadowed": map[string]interface{}{
					"virtlet-flexvolume.json": utils.ToJSONUnindented(cephJSONVolumeInfo),
				},
			},
			mountJournal: []*testutils.Record{
				{
					Name:  "Mount",
					Value: []interface{}{"tmpfs", cephDir, "tmpfs", false},
				},
			},
		},
		{
			name:   "unmount-ceph",
			args:   []string{"unmount", cephDir},
			status: "Success",
			subdir: "ceph",
			mountJournal: []*testutils.Record{
				{
					Name:  "Unmount",
					Value: []interface{}{cephDir, true},
				},
			},
		},
		{
			name:   "mount-ceph-1",
			args:   []string{"mount", cephDir, utils.ToJSON(cephJSONOpts)},
			status: "Success",
			subdir: "ceph",
			files: map[string]interface{}{
				"virtlet-flexvolume.json": utils.ToJSONUnindented(cephJSONVolumeInfo),
				".shadowed": map[string]interface{}{
					"virtlet-flexvolume.json": utils.ToJSONUnindented(cephJSONVolumeInfo),
				},
			},
			mountJournal: []*testutils.Record{
				{
					Name:  "Mount",
					Value: []interface{}{"tmpfs", cephDir, "tmpfs", false},
				},
			},
		},
		{
			name:   "unmount-ceph-1",
			args:   []string{"unmount", cephDir},
			status: "Success",
			subdir: "ceph",
			mountJournal: []*testutils.Record{
				{
					Name:  "Unmount",
					Value: []interface{}{cephDir, true},
				},
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
			rec := testutils.NewToplevelRecorder()
			fs := fakefs.NewFakeFileSystem(t, rec, tmpDir, nil)
			d := NewDriver(func() string {
				return fakeUUID
			}, fs)
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
					t.Errorf("bad file content.\n%s\n-- instead of --\n%s", utils.ToJSON(files), utils.ToJSON(step.files))
				}
			}
			if !reflect.DeepEqual(rec.Content(), step.mountJournal) {
				t.Errorf("unexpected mount journal: %#v instead of %#v", rec.Content(), step.mountJournal)
			}
		})
	}
}

// TODO: escape xml in iso path
