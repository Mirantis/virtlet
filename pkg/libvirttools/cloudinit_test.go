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
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"

	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

type fakeFlexvolume struct {
	uuid string
	part int
	path string
}

func newFakeFlexvolume(t *testing.T, parentDir string, uuid string, part int) *fakeFlexvolume {
	info := map[string]string{"uuid": uuid}
	if part >= 0 {
		info["part"] = strconv.Itoa(part)
	}
	volDir := filepath.Join(parentDir, uuid)
	if err := os.MkdirAll(volDir, 0777); err != nil {
		t.Fatalf("MkdirAll(): %q: %v", volDir, err)
	}
	infoPath := filepath.Join(volDir, "virtlet-flexvolume.json")
	if err := utils.WriteJson(infoPath, info, 0777); err != nil {
		t.Fatalf("WriteJson(): %q: %v", infoPath, err)
	}
	return &fakeFlexvolume{
		uuid: uuid,
		part: part,
		path: volDir,
	}
}

func TestCloudInitGenerator(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "fake-flexvol")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)
	vols := []*fakeFlexvolume{
		newFakeFlexvolume(t, tmpDir, "77f29a0e-46af-4188-a6af-9ff8b8a65224", -1),
		newFakeFlexvolume(t, tmpDir, "82b7a880-dc04-48a3-8f2d-0c6249bb53fe", 0),
		newFakeFlexvolume(t, tmpDir, "94ae25c7-62e1-4854-9f9b-9e285c3a5ed9", 2),
	}

	for _, tc := range []struct {
		name                string
		config              *VMConfig
		volumeMap           map[string]string
		expectedMetaData    map[string]interface{}
		expectedUserData    map[string]interface{}
		expectedUserDataStr string
	}{
		{
			name: "plain pod",
			config: &VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &VirtletAnnotations{},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
			},
			expectedUserData: nil,
		},
		{
			name: "pod with ssh keys",
			config: &VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &VirtletAnnotations{
					SSHKeys: []string{"key1", "key2"},
				},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
				"public-keys":    []interface{}{"key1", "key2"},
			},
			expectedUserData: nil,
		},
		{
			name: "pod with ssh keys and meta-data override",
			config: &VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &VirtletAnnotations{
					SSHKeys: []string{"key1", "key2"},
					MetaData: map[string]interface{}{
						"instance-id": "foobar",
					},
				},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foobar",
				"local-hostname": "foo",
				"public-keys":    []interface{}{"key1", "key2"},
			},
			expectedUserData: nil,
		},
		{
			name: "pod with user data",
			config: &VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &VirtletAnnotations{
					UserData: map[string]interface{}{
						"users": []interface{}{
							map[string]interface{}{
								"name": "cloudy",
							},
						},
					},
					SSHKeys: []string{"key1", "key2"},
				},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
				"public-keys":    []interface{}{"key1", "key2"},
			},
			expectedUserData: map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{
						"name": "cloudy",
					},
				},
			},
		},
		{
			name: "pod with env variables",
			config: &VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &VirtletAnnotations{},
				Environment: []*VMKeyValue{
					{"foo", "bar"},
					{"baz", "abc"},
				},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
			},
			expectedUserData: map[string]interface{}{
				"write_files": []interface{}{
					map[string]interface{}{
						"path":    "/etc/cloud/environment",
						"content": "foo=bar\nbaz=abc\n",
					},
				},
			},
		},
		{
			name: "pod with env variables and user data",
			config: &VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &VirtletAnnotations{
					UserData: map[string]interface{}{
						"users": []interface{}{
							map[string]interface{}{
								"name": "cloudy",
							},
						},
						"write_files": []interface{}{
							map[string]interface{}{
								"path":    "/etc/foobar",
								"content": "whatever",
							},
						},
					},
				},
				Environment: []*VMKeyValue{
					{"foo", "bar"},
					{"baz", "abc"},
				},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
			},
			expectedUserData: map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{
						"name": "cloudy",
					},
				},
				"write_files": []interface{}{
					map[string]interface{}{
						"path":    "/etc/foobar",
						"content": "whatever",
					},
					map[string]interface{}{
						"path":    "/etc/cloud/environment",
						"content": "foo=bar\nbaz=abc\n",
					},
				},
			},
		},
		{
			name: "pod with user data script",
			config: &VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &VirtletAnnotations{
					UserDataScript: "#!/bin/sh\necho hi\n",
					SSHKeys:        []string{"key1", "key2"},
				},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
				"public-keys":    []interface{}{"key1", "key2"},
			},
			expectedUserDataStr: "#!/bin/sh\necho hi\n",
		},
		{
			name: "pod with volumes to mount",
			config: &VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &VirtletAnnotations{},
				Mounts: []*VMMount{
					{
						ContainerPath: "/opt",
						HostPath:      vols[0].path,
					},
					{
						ContainerPath: "/var/lib/whatever",
						HostPath:      vols[1].path,
					},
					{
						ContainerPath: "/var/lib/foobar",
						HostPath:      vols[2].path,
					},
				},
			},
			volumeMap: map[string]string{
				vols[0].uuid: "/dev/sdb",
				vols[1].uuid: "/dev/sdc",
				vols[2].uuid: "/dev/sdd",
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
			},
			expectedUserData: map[string]interface{}{
				"mounts": []interface{}{
					[]interface{}{"/dev/sdb1", "/opt"},
					[]interface{}{"/dev/sdc", "/var/lib/whatever"},
					[]interface{}{"/dev/sdd2", "/var/lib/foobar"},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewCloudInitGenerator(tc.config, tc.volumeMap)

			metaDataBytes, err := g.generateMetaData()
			if err != nil {
				t.Fatalf("GenerateMetaData(): %v", err)
			}
			var metaData map[string]interface{}
			if err := json.Unmarshal(metaDataBytes, &metaData); err != nil {
				t.Fatalf("Can't unmarshal meta-data: %v", err)
			}

			if !reflect.DeepEqual(tc.expectedMetaData, metaData) {
				t.Errorf("Bad meta-data:\n%s\nUnmarshaled:\n%s", metaDataBytes, spew.Sdump(metaData))
			}
			userDataBytes, err := g.generateUserData()
			if err != nil {
				t.Fatalf("GenerateUserData(): %v", err)
			}
			if tc.expectedUserDataStr != "" {
				if string(userDataBytes) != tc.expectedUserDataStr {
					t.Errorf("Bad user-data string:\n%s", userDataBytes)
				}
			} else {
				if !bytes.HasPrefix(userDataBytes, []byte("#cloud-config\n")) {
					t.Errorf("No #cloud-config header")
				}
				var userData map[string]interface{}
				if err := yaml.Unmarshal(userDataBytes, &userData); err != nil {
					t.Fatalf("Can't unmarshal user-data: %v", err)
				}

				if !reflect.DeepEqual(tc.expectedUserData, userData) {
					t.Errorf("Bad user-data:\n%s\nUnmarshaled:\n%s", userDataBytes, spew.Sdump(userData))
				}
			}
		})
	}
}

func TestGenerateDisk(t *testing.T) {
	g := NewCloudInitGenerator(&VMConfig{
		PodName:           "foo",
		PodNamespace:      "default",
		ParsedAnnotations: &VirtletAnnotations{},
	}, nil)
	isoPath, diskDef, err := g.GenerateDisk()
	if err != nil {
		t.Fatalf("GenerateDisk(): %v", err)
	}
	if !reflect.DeepEqual(diskDef, &libvirtxml.DomainDisk{
		Type:     "file",
		Device:   "cdrom",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: isoPath},
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	}) {
		t.Errorf("Bad disk definition:\n%s", spew.Sdump(diskDef))
	}
	m, err := testutils.IsoToMap(isoPath)
	if err != nil {
		t.Fatalf("IsoToMap(): %v", err)
	}
	if !reflect.DeepEqual(m, map[string]interface{}{
		"meta-data": "{\"instance-id\":\"foo.default\",\"local-hostname\":\"foo\"}",
		"user-data": "#cloud-config\n",
	}) {
		t.Errorf("bad iso content:\n%s", spew.Sdump(m))
	}
}
