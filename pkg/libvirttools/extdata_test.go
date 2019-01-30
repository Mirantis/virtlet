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

package libvirttools

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/utils"
)

func withExternalDataLoader(loader types.ExternalDataLoader, toCall func()) {
	oldLoader := types.GetExternalDataLoader()
	defer func() {
		types.SetExternalDataLoader(oldLoader)
	}()
	types.SetExternalDataLoader(loader)
	toCall()
}

func TestLoadExternalUserData(t *testing.T) {
	fc := fakekube.NewSimpleClientset(
		&v1.ConfigMap{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "samplecfg",
				Namespace: "testns",
			},
			Data: map[string]string{
				"foo": "bar",
				"baz": "foobar",
			},
		},
		&v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "samplesecret",
				Namespace: "testns",
			},
			Data: map[string][]byte{
				"foo": []byte("topSecretBar"),
				"baz": []byte("topSecretFoobar"),
			},
		},
		&v1.ConfigMap{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "samplecfg-keys",
				Namespace: "testns",
			},
			Data: map[string]string{
				"authorized_keys": "key1\nkey2\n",
			},
		},
		&v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "samplesecret-keys",
				Namespace: "testns",
			},
			Data: map[string][]byte{
				"authorized_keys": []byte("secretKey1\nsecretKey2\n"),
			},
		},
		&v1.ConfigMap{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "samplecfg-keys-custom",
				Namespace: "testns",
			},
			Data: map[string]string{
				"keys": "key1\nkey2\n",
			},
		},
		&v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "samplesecret-keys-custom",
				Namespace: "testns",
			},
			Data: map[string][]byte{
				"keys": []byte("secretKey1\nsecretKey2\n"),
			},
		},
		&v1.ConfigMap{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "filemapcfg",
				Namespace: "testns",
			},
			Data: map[string]string{
				"file1":          "ZmlsZTEgY29udGVudA==", // "file1 content"
				"file1_path":     "/etc/foo.conf",
				"file2":          "file2 content",
				"file2_path":     "/etc/bar/bar.conf",
				"file2_encoding": "plain",
				"file3":          "ZmlsZTMgY29udGVudA==", // "file3 content"
				"file3_path":     "/etc/baz/baz.conf",
				"file3_encoding": "base64",
			},
		},
		&v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "filemapsecret",
				Namespace: "testns",
			},
			Data: map[string][]byte{
				"file1":          []byte("ZmlsZTEgc2VjcmV0IGNvbnRlbnQ="), // "file1 secret content"
				"file1_path":     []byte("/etc/foo.conf"),
				"file2":          []byte("file2 secret content"),
				"file2_path":     []byte("/etc/bar/bar.conf"),
				"file2_encoding": []byte("plain"),
				"file3":          []byte("ZmlsZTMgc2VjcmV0IGNvbnRlbnQ="), // "file3 secret content"
				"file3_path":     []byte("/etc/baz/baz.conf"),
				"file3_encoding": []byte("base64"),
			},
		})
	for _, tc := range []struct {
		name             string
		podAnnotations   map[string]string
		expectedUserData map[string]interface{}
		expectedSSHKeys  []string
		expectedFiles    map[string][]byte
	}{
		{
			name: "user data from configmap",
			podAnnotations: map[string]string{
				"VirtletCloudInitUserDataSource": "configmap/samplecfg",
			},
			expectedUserData: map[string]interface{}{
				"foo": "bar",
				"baz": "foobar",
			},
		},
		{
			name: "user data from secret",
			podAnnotations: map[string]string{
				"VirtletCloudInitUserDataSource": "secret/samplesecret",
			},
			expectedUserData: map[string]interface{}{
				"foo": "topSecretBar",
				"baz": "topSecretFoobar",
			},
		},
		{
			name: "ssh keys from configmap (default key)",
			podAnnotations: map[string]string{
				"VirtletSSHKeySource": "configmap/samplecfg-keys",
			},
			expectedSSHKeys: []string{"key1", "key2"},
		},
		{
			name: "user data from secret (default key)",
			podAnnotations: map[string]string{
				"VirtletSSHKeySource": "secret/samplesecret-keys",
			},
			expectedSSHKeys: []string{"secretKey1", "secretKey2"},
		},
		{
			name: "ssh keys from configmap (custom key)",
			podAnnotations: map[string]string{
				"VirtletSSHKeySource": "configmap/samplecfg-keys-custom/keys",
			},
			expectedSSHKeys: []string{"key1", "key2"},
		},
		{
			name: "user data from secret (custom key)",
			podAnnotations: map[string]string{
				"VirtletSSHKeySource": "secret/samplesecret-keys-custom/keys",
			},
			expectedSSHKeys: []string{"secretKey1", "secretKey2"},
		},
		{
			name: "files from configmap",
			podAnnotations: map[string]string{
				"VirtletFilesFromDataSource": "configmap/filemapcfg",
			},
			expectedFiles: map[string][]byte{
				"/etc/foo.conf":     []byte("file1 content"),
				"/etc/bar/bar.conf": []byte("file2 content"),
				"/etc/baz/baz.conf": []byte("file3 content"),
			},
		},
		{
			name: "files from secret",
			podAnnotations: map[string]string{
				"VirtletFilesFromDataSource": "secret/filemapsecret",
			},
			expectedFiles: map[string][]byte{
				"/etc/foo.conf":     []byte("file1 secret content"),
				"/etc/bar/bar.conf": []byte("file2 secret content"),
				"/etc/baz/baz.conf": []byte("file3 secret content"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withExternalDataLoader(&DefaultExternalDataLoader{kubeClient: fc}, func() {
				vmc := &types.VMConfig{
					PodNamespace:   "testns",
					PodAnnotations: tc.podAnnotations,
				}
				if err := vmc.LoadAnnotations(); err != nil {
					t.Fatalf("LoadAnnotations(): %v", err)
				}

				if tc.expectedUserData != nil && !reflect.DeepEqual(tc.expectedUserData, vmc.ParsedAnnotations.UserData) {
					t.Errorf("bad user data. Expected:\n%s\nGot:\n%s", utils.ToJSON(tc.expectedUserData), utils.ToJSON(vmc.ParsedAnnotations.UserData))
				}

				if tc.expectedSSHKeys != nil && !reflect.DeepEqual(tc.expectedSSHKeys, vmc.ParsedAnnotations.SSHKeys) {
					t.Errorf("bad ssh keys. Expected:\n%s\nGot:\n%s", utils.ToJSON(tc.expectedSSHKeys), utils.ToJSON(vmc.ParsedAnnotations.SSHKeys))
				}

				if tc.expectedFiles != nil && !reflect.DeepEqual(tc.expectedFiles, vmc.ParsedAnnotations.InjectedFiles) {
					t.Errorf("bad ssh keys. Expected:\n%s\nGot:\n%s", utils.ToJSON(tc.expectedFiles), utils.ToJSON(vmc.ParsedAnnotations.InjectedFiles))
				}
			})
		})
	}
}
