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
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

func TestCloudInitGenerator(t *testing.T) {
	for _, tc := range []struct {
		name                string
		podName             string
		podNs               string
		annotations         *VirtletAnnotations
		expectedMetaData    map[string]interface{}
		expectedUserData    map[string]interface{}
		expectedUserDataStr string
	}{
		{
			name:        "plain pod",
			podName:     "foo",
			podNs:       "default",
			annotations: &VirtletAnnotations{},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
			},
			expectedUserData: nil,
		},
		{
			name:    "pod with ssh keys",
			podName: "foo",
			podNs:   "default",
			annotations: &VirtletAnnotations{
				SSHKeys: []string{"key1", "key2"},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
				"public-keys":    []interface{}{"key1", "key2"},
			},
			expectedUserData: nil,
		},
		{
			name:    "pod with ssh keys and meta-data override",
			podName: "foo",
			podNs:   "default",
			annotations: &VirtletAnnotations{
				SSHKeys: []string{"key1", "key2"},
				MetaData: map[string]interface{}{
					"instance-id": "foobar",
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
			name:    "pod with user data",
			podName: "foo",
			podNs:   "default",
			annotations: &VirtletAnnotations{
				UserData: map[string]interface{}{
					"users": []interface{}{
						map[string]interface{}{
							"name": "cloudy",
						},
					},
				},
				SSHKeys: []string{"key1", "key2"},
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
			name:    "pod with user data script",
			podName: "foo",
			podNs:   "default",
			annotations: &VirtletAnnotations{
				UserDataScript: "#!/bin/sh\necho hi\n",
				SSHKeys:        []string{"key1", "key2"},
			},
			expectedMetaData: map[string]interface{}{
				"instance-id":    "foo.default",
				"local-hostname": "foo",
				"public-keys":    []interface{}{"key1", "key2"},
			},
			expectedUserDataStr: "#!/bin/sh\necho hi\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewCloudInitGenerator(tc.podName, tc.podNs, tc.annotations)

			metaDataStr, err := g.generateMetaData()
			if err != nil {
				t.Fatalf("GenerateMetaData(): %v", err)
			}
			var metaData map[string]interface{}
			if err := json.Unmarshal([]byte(metaDataStr), &metaData); err != nil {
				t.Fatalf("Can't unmarshal meta-data: %v", err)
			}

			if !reflect.DeepEqual(tc.expectedMetaData, metaData) {
				t.Errorf("Bad meta-data:\n%s\nUnmarshaled:\n%s", metaDataStr, spew.Sdump(metaData))
			}
			userDataStr, err := g.generateUserData()
			if err != nil {
				t.Fatalf("GenerateUserData(): %v", err)
			}
			if tc.expectedUserDataStr != "" {
				if userDataStr != tc.expectedUserDataStr {
					t.Errorf("Bad user-data string:\n%s", userDataStr)
				}
			} else {
				if !strings.HasPrefix(userDataStr, "#cloud-config\n") {
					t.Errorf("No #cloud-config header")
				}
				var userData map[string]interface{}
				if err := yaml.Unmarshal([]byte(userDataStr), &userData); err != nil {
					t.Fatalf("Can't unmarshal user-data: %v", err)
				}

				if !reflect.DeepEqual(tc.expectedUserData, userData) {
					t.Errorf("Bad user-data:\n%s\nUnmarshaled:\n%s", userDataStr, spew.Sdump(userData))
				}
			}
		})
	}
}

func TestGenerateDisk(t *testing.T) {
	g := NewCloudInitGenerator("foo", "default", &VirtletAnnotations{})
	isoPath, diskDef, err := g.GenerateDisk()
	if err != nil {
		t.Fatalf("GenerateDisk(): %v", err)
	}
	if !reflect.DeepEqual(diskDef, &libvirtxml.DomainDisk{
		Type:     "file",
		Device:   "disk",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: isoPath},
		Target:   &libvirtxml.DomainDiskTarget{Bus: "virtio"},
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
