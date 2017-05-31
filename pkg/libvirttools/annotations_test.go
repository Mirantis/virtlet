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
	"reflect"
	"testing"
)

func TestVirtletAnnotations(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		annotations map[string]string
		// va being nil means invalid annotations
		va *VirtletAnnotations
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			va:          &VirtletAnnotations{VCPUCount: 1},
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			va:          &VirtletAnnotations{VCPUCount: 1},
		},
		{
			name:        "negative vcpu count (default)",
			annotations: map[string]string{"VirtletVCPUCount": "-1"},
			va:          &VirtletAnnotations{VCPUCount: 1},
		},
		{
			name:        "zero vcpu count (default)",
			annotations: map[string]string{"VirtletVCPUCount": "0"},
			va:          &VirtletAnnotations{VCPUCount: 1},
		},
		{
			name:        "vcpu count specified",
			annotations: map[string]string{"VirtletVCPUCount": "4"},
			va:          &VirtletAnnotations{VCPUCount: 4},
		},
		{
			name: "vcpu count and volumes",
			annotations: map[string]string{
				"VirtletVCPUCount": "4",
				"VirtletVolumes":   `[{"Name": "vol1"}, {"Name": "vol2", "Format": "qcow2", "Capacity": "2", "CapacityUnit": "MB"}, {"Name": "vol3"}]`,
			},
			va: &VirtletAnnotations{
				VCPUCount: 4,
				Volumes: []*VirtletVolume{
					{
						Name:         "vol1",
						Format:       "qcow2",
						Capacity:     1024,
						CapacityUnit: "MB",
					},
					{
						Name:         "vol2",
						Format:       "qcow2",
						Capacity:     2,
						CapacityUnit: "MB",
					},
					{
						Name:         "vol3",
						Format:       "qcow2",
						Capacity:     1024,
						CapacityUnit: "MB",
					},
				},
			},
		},
		{
			name: "raw volumes",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "rawVol", "Format": "rawDevice", "Path": "/dev/sdb"}]`,
			},
			va: &VirtletAnnotations{
				VCPUCount: 1,
				Volumes: []*VirtletVolume{
					{
						Name:   "rawVol",
						Format: "rawDevice",
						Path:   "/dev/sdb",
					},
				},
			},
		},
		{
			name: "cloud-init yaml and ssh keys",
			annotations: map[string]string{
				"VirtletCloudInitMetaData": `
                                  instance-id: foobar`,
				"VirtletCloudInitUserData": `
                                  users:
                                  - name: cloudy`,
				// empty lines are ignored
				"VirtletSSHKeys": "key1\n\nkey2\n",
			},
			va: &VirtletAnnotations{
				VCPUCount: 1,
				MetaData: map[string]interface{}{
					"instance-id": "foobar",
				},
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
		{
			name: "cloud-init user data overwrite set",
			annotations: map[string]string{
				"VirtletCloudInitUserDataOverwrite": "true",
			},
			va: &VirtletAnnotations{
				VCPUCount:         1,
				UserDataOverwrite: true,
			},
		},
		{
			name: "cloud-init user data script",
			annotations: map[string]string{
				"VirtletCloudInitUserDataScript": "#!/bin/sh\necho hi\n",
			},
			va: &VirtletAnnotations{
				VCPUCount:      1,
				UserDataScript: "#!/bin/sh\necho hi\n",
			},
		},
		// bad metadata items follow
		{
			name:        "bad vcpu count",
			annotations: map[string]string{"VirtletVCPUCount": "256"},
		},
		{
			name: "bad volume json",
			annotations: map[string]string{
				"VirtletVolumes": `[{`,
			},
		},
		{
			name: "volume without name",
			annotations: map[string]string{
				"VirtletVolumes": `[{}]`,
			},
		},
		{
			name: "bad volume - unknown format",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "badvol", "Format": "bad"}]`,
			},
		},
		{
			name: "bad volume - path for qcow2",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "badvol", "Path": "/dev/whatever"}]`,
			},
		},
		{
			name: "bad volume - capacity specified for a raw device",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "rawVol", "Format": "rawDevice", "Capacity": "1024", "Path": "/dev/sdb"}]`,
			},
		},
		{
			name: "bad volume - capacity unit specified for a raw device",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "rawVol", "Format": "rawDevice", "CapacityUnit": "B", "Path": "/dev/sdb"}]`,
			},
		},
		{
			name: "bad volume - raw volume path doesn't start with /dev/",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "rawVol", "Format": "rawDevice", "Path": "/tmp/foobar"}]`,
			},
		},
		{
			name: "bad volume - bad capacity units",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "badvol", "CapacityUnit": "cm"}]`,
			},
		},
		{
			name: "bad volume - negative capacity",
			annotations: map[string]string{
				"VirtletVolumes": `[{"Name": "badvol", "Capacity": "-1024"}]`,
			},
		},
		{
			name: "bad cloud-init meta-data",
			annotations: map[string]string{
				"VirtletCloudInitMetaData": "{",
			},
		},
		{
			name: "bad cloud-init user-data",
			annotations: map[string]string{
				"VirtletCloudInitUserData": "{",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			va, err := LoadAnnotations(testCase.annotations)
			switch {
			case testCase.va == nil && err == nil:
				t.Errorf("invalid annotations considered valid:\n%#v", testCase.annotations)
			case testCase.va != nil && err != nil:
				t.Errorf("unexpected error %q loading annotations:\n%#v", err, testCase.annotations)
			case testCase.va != nil:
				if !reflect.DeepEqual(testCase.va, va) {
					t.Errorf("virtlet annotations mismatch: got\n%#v\ninstead of\n%#v", va, testCase.va)
				}
			}
		})
	}
}
