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
	"log"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	cephDisk = `
<disk type="network" device="disk">
  <driver name="qemu" type="raw"/>
  <auth username="libvirt">
    <secret type="ceph" uuid="abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"/>
  </auth>
  <source protocol="rbd" name="libvirt-pool/rbd-test-image">
    <host name="127.0.0.1" port="6789"/>
  </source>
  <target dev="%s" bus="virtio"/>
</disk>
`
	cephSecret = `
<secret ephemeral='no' private='no'>
  <uuid>abb67e3c-71b3-4ddd-5505-8c4215d5c4eb</uuid>
  <usage type='ceph'>
    <name>libvirt</name>
  </usage>
</secret>
`
)

// dirToMap converts directory to a map where keys are filenames
// without full path and values are the contents of the files.  If the
// directory doesn't exist, dirToMap returns nil
func dirToMap(dir string) (map[string]string, error) {
	if fi, err := os.Stat(dir); err != nil && os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("os.Stat(): %v", err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%q expected to be a directory", dir)
	}
	paths, err := filepath.Glob(dir + "/*")
	if err != nil {
		return nil, fmt.Errorf("filepath.Glob(): %v", err)
	}
	m := map[string]string{}
	for _, p := range paths {
		filename := filepath.Base(p)
		if fi, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("os.Stat(): %v", err)
		} else if fi.IsDir() {
			return nil, fmt.Errorf("unexpected directory: %q", filename)
		}
		bs, err := ioutil.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("ioutil.ReadFile(): %v", err)
		}
		m[filename] = string(bs)
	}
	return m, nil
}

func mapToJson(m map[string]string) string {
	bs, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		log.Panicf("error marshalling json: %v", err)
	}
	return string(bs)
}

func TestFlexVolume(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "flexvolume-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cephJsonOpts := mapToJson(map[string]string{
		"monitor": "127.0.0.1:6789",
		"pool":    "libvirt-pool",
		"volume":  "rbd-test-image",
		"secret":  "foobar",
		"user":    "libvirt",
	})
	for _, step := range []struct {
		name    string
		args    []string
		subdir  string
		status  string
		message string
		fields  map[string]interface{}
		files   map[string]string
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
				"{}",
				"node1",
			},
			status: "Success",
		},
		{
			name: "getvolumename",
			args: []string{
				"getvolumename",
				cephJsonOpts,
			},
			status: "Success",
			fields: map[string]interface{}{
				"volumeName": "127.0.0.1/6789/libvirt-pool/rbd-test-image/libvirt",
			},
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
			name: "mount",
			args: []string{
				"mount",
				path.Join(tmpDir, "ceph"),
				cephJsonOpts,
			},
			status: "Success",
			subdir: "ceph",
			files: map[string]string{
				"disk.xml":   cephDisk,
				"key":        "foobar",
				"secret.xml": cephSecret,
			},
		},
		{
			name: "unmount",
			args: []string{
				"unmount",
				path.Join(tmpDir, "ceph"),
			},
			status: "Success",
			subdir: "ceph",
			files:  nil, // dir must be removed
		},
		{
			name: "mount",
			args: []string{
				"mount",
				path.Join(tmpDir, "ceph"),
				cephJsonOpts,
			},
			status: "Success",
			subdir: "ceph",
			files: map[string]string{
				"disk.xml":   cephDisk,
				"key":        "foobar",
				"secret.xml": cephSecret,
			},
		},
		{
			name: "unmount",
			args: []string{
				"unmount",
				path.Join(tmpDir, "ceph"),
			},
			status: "Success",
			subdir: "ceph",
			files:  nil, // dir must be removed
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
			d := NewFlexVolumeDriver(func() string {
				return "abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"
			})
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
				files, err := dirToMap(path.Join(tmpDir, step.subdir))
				if err != nil {
					t.Fatalf("dirToMap() on %q: %v", subdir, err)
				}
				if !reflect.DeepEqual(files, step.files) {
					t.Errorf("bad file content.\n%s\n-- instead of --\n%s", mapToJson(files), mapToJson(step.files))
				}
			}
		})
	}
}
