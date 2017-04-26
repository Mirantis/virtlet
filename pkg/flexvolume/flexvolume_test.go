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
	"reflect"
	"strings"
	"testing"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
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
	noCloudDiskTestTemplate = `
<disk type="file" device="disk">
  <driver name="qemu" type="raw"/>
  <source file='%s'/>
  <readonly/>
  <target dev="%%s" bus="virtio"/>
</disk>
`
	noCloudMetaData = `
instance-id: some-instance-id
local-hostname: foobar
`
	noCloudUserData = `
    #cloud-config
    fqdn: ubuntu-16-vm.mydomain.com
    users:
      - name: root
        ssh-authorized-keys:
          - ssh-rsa YOUR_KEY_HEWE me@localhost
    ssh_pwauth: True
    runcmd:
    - [ apt-get, update ]
    - [ apt-get, install, -y, --force-yes, apache2 ]
`
)

func mapToJson(m map[string]interface{}) string {
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

	cephJsonOpts := mapToJson(map[string]interface{}{
		"type":    "ceph",
		"monitor": "127.0.0.1:6789",
		"pool":    "libvirt-pool",
		"volume":  "rbd-test-image",
		"secret":  "foobar",
		"user":    "libvirt",
	})
	noCloudJsonOpts := mapToJson(map[string]interface{}{
		"type":     "nocloud",
		"metadata": noCloudMetaData,
		"userdata": noCloudUserData,
	})
	noCloudDisk := fmt.Sprintf(noCloudDiskTestTemplate, path.Join(tmpDir, "nocloud/cidata.iso"))
	for _, step := range []struct {
		name    string
		args    []string
		subdir  string
		status  string
		message string
		fields  map[string]interface{}
		files   map[string]interface{}
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
			name: "mount-ceph",
			args: []string{
				"mount",
				path.Join(tmpDir, "ceph"),
				cephJsonOpts,
			},
			status: "Success",
			subdir: "ceph",
			files: map[string]interface{}{
				"disk.xml":   cephDisk,
				"key":        "foobar",
				"secret.xml": cephSecret,
			},
		},
		{
			name: "unmount-ceph",
			args: []string{
				"unmount",
				path.Join(tmpDir, "ceph"),
			},
			status: "Success",
			subdir: "ceph",
			files:  nil, // dir must be removed
		},
		{
			name: "mount-ceph-1",
			args: []string{
				"mount",
				path.Join(tmpDir, "ceph"),
				cephJsonOpts,
			},
			status: "Success",
			subdir: "ceph",
			files: map[string]interface{}{
				"disk.xml":   cephDisk,
				"key":        "foobar",
				"secret.xml": cephSecret,
			},
		},
		{
			name: "unmount-ceph-1",
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
		{
			name: "mount-nocloud",
			args: []string{
				"mount",
				path.Join(tmpDir, "nocloud"),
				noCloudJsonOpts,
			},
			status: "Success",
			subdir: "nocloud",
			files: map[string]interface{}{
				"disk.xml": noCloudDisk,
				"cidata.cd": map[string]interface{}{
					"meta-data": noCloudMetaData,
					"user-data": noCloudUserData,
				},
			},
		},
		{
			name: "unmount-nocloud",
			args: []string{
				"unmount",
				path.Join(tmpDir, "nocloud"),
			},
			status: "Success",
			subdir: "nocloud",
			files:  nil, // dir must be removed
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
				files, err := testutils.DirToMap(path.Join(tmpDir, step.subdir))
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

// TODO: escape xml in iso path
