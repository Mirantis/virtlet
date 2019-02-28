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

package types

import (
	"reflect"
	"testing"

	uuid "github.com/nu7hatch/gouuid"
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
			va: &VirtletAnnotations{
				VCPUCount:   1,
				DiskDriver:  "scsi",
				CDImageType: "nocloud",
			},
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			va: &VirtletAnnotations{
				VCPUCount:   1,
				DiskDriver:  "scsi",
				CDImageType: "nocloud",
			},
		},
		{
			name:        "non empty cloud init type annotation",
			annotations: map[string]string{"VirtletCloudInitImageType": "configdrive"},
			va: &VirtletAnnotations{
				VCPUCount:   1,
				DiskDriver:  "scsi",
				CDImageType: "configdrive",
			},
		},
		{
			name:        "negative vcpu count (default)",
			annotations: map[string]string{"VirtletVCPUCount": "-1"},
			va: &VirtletAnnotations{
				VCPUCount:   1,
				DiskDriver:  "scsi",
				CDImageType: "nocloud",
			},
		},
		{
			name:        "zero vcpu count (default)",
			annotations: map[string]string{"VirtletVCPUCount": "0"},
			va: &VirtletAnnotations{
				VCPUCount:   1,
				DiskDriver:  "scsi",
				CDImageType: "nocloud",
			},
		},
		{
			name:        "vcpu count specified",
			annotations: map[string]string{"VirtletVCPUCount": "4"},
			va: &VirtletAnnotations{
				VCPUCount:   4,
				DiskDriver:  "scsi",
				CDImageType: "nocloud",
			},
		},
		{
			name:        "root volume size",
			annotations: map[string]string{"VirtletRootVolumeSize": "1Gi"},
			va: &VirtletAnnotations{
				VCPUCount:      1,
				DiskDriver:     "scsi",
				CDImageType:    "nocloud",
				RootVolumeSize: 1073741824,
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
				SSHKeys:     []string{"key1", "key2"},
				DiskDriver:  "scsi",
				CDImageType: "nocloud",
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
				DiskDriver:        "scsi",
				CDImageType:       "nocloud",
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
				DiskDriver:     "scsi",
				CDImageType:    "nocloud",
			},
		},
		{
			name: "system UUID",
			annotations: map[string]string{
				"VirtletSystemUUID": "53008994-44c0-4017-ad44-9c49758083da",
			},
			va: &VirtletAnnotations{
				VCPUCount: 1,
				SystemUUID: &uuid.UUID{
					0x53, 0, 0x89, 0x94,
					0x44, 0xc0, 0x40, 0x17, 0xad, 0x44,
					0x9c, 0x49, 0x75, 0x80, 0x83, 0xda,
				},
				DiskDriver:  "scsi",
				CDImageType: "nocloud",
			},
		},
		{
			name: "force DHCP network config",
			annotations: map[string]string{
				"VirtletForceDHCPNetworkConfig": "true",
			},
			va: &VirtletAnnotations{
				VCPUCount:              1,
				DiskDriver:             "scsi",
				CDImageType:            "nocloud",
				ForceDHCPNetworkConfig: true,
			},
		},
		// bad metadata items follow
		{
			name:        "bad vcpu count",
			annotations: map[string]string{"VirtletVCPUCount": "256"},
		},
		{
			name:        "bad disk driver",
			annotations: map[string]string{"VirtletDiskDriver": "ducttape"},
		},
		{
			name: "bad cloud-init meta-data",
			annotations: map[string]string{
				"VirtletCloudInitMetaData": "{",
			},
		},
		{
			name: "bad cloud-init image type",
			annotations: map[string]string{
				"VirtletCloudInitImageType": "ducttape",
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
			va, err := loadAnnotations("", testCase.annotations)
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
