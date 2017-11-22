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
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
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

func buildNetworkedPodConfig(cniResult *cnicurrent.Result) *VMConfig {
	r, err := json.Marshal(cniResult)
	if err != nil {
		panic("failed to marshal CNI result")
	}
	return &VMConfig{
		PodName:           "foo",
		PodNamespace:      "default",
		ParsedAnnotations: &VirtletAnnotations{},
		CNIConfig:         string(r),
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
		name                  string
		config                *VMConfig
		volumeMap             map[string]string
		expectedMetaData      map[string]interface{}
		expectedUserData      map[string]interface{}
		expectedNetworkConfig map[string]interface{}
		expectedUserDataStr   string
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
			expectedNetworkConfig: map[string]interface{}{
				// that's how yaml parses the number
				"version": float64(1),
			},
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
		{
			name: "pod with network config",
			config: buildNetworkedPodConfig(&cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "cni0",
						Mac:     "00:11:22:33:44:55",
						Sandbox: "/var/run/netns/bae464f1-6ee7-4ee2-826e-33293a9de95e",
					},
					{
						Name:    "ignoreme0",
						Mac:     "00:12:34:56:78:9a",
						Sandbox: "", // host interface
					},
				},
				IPs: []*cnicurrent.IPConfig{
					{
						Version: "4",
						Address: net.IPNet{
							IP:   net.IPv4(1, 1, 1, 1),
							Mask: net.CIDRMask(8, 32),
						},
						Gateway:   net.IPv4(1, 2, 3, 4),
						Interface: 0,
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IPv4zero,
							Mask: net.CIDRMask(0, 32),
						},
						GW: nil,
					},
				},
				DNS: cnitypes.DNS{
					Nameservers: []string{"1.2.3.4"},
					Search:      []string{"some", "search"},
				},
			}),
			expectedNetworkConfig: map[string]interface{}{
				"version": float64(1),
				"config": []interface{}{
					map[string]interface{}{
						"mac_address": "00:11:22:33:44:55",
						"name":        "cni0",
						"subnets": []interface{}{
							map[string]interface{}{
								"address":         "1.1.1.1/8",
								"dns_nameservers": []interface{}{"1.2.3.4"},
								"dns_search":      []interface{}{"some", "search"},
								"type":            "static",
							},
						},
						"type": "physical",
					},
					map[string]interface{}{
						"destination": "0.0.0.0/0",
						"gateway":     "1.2.3.4",
						"type":        "route",
					},
				},
			},
		},
		{
			name: "pod with multiple network interfaces",
			config: buildNetworkedPodConfig(&cnicurrent.Result{
				Interfaces: []*cnicurrent.Interface{
					{
						Name:    "cni0",
						Mac:     "00:11:22:33:44:55",
						Sandbox: "/var/run/netns/bae464f1-6ee7-4ee2-826e-33293a9de95e",
					},
					{
						Name:    "cni1",
						Mac:     "00:11:22:33:ab:cd",
						Sandbox: "/var/run/netns/d920d2e2-5849-4c70-b9a6-5e3cb4f831cb",
					},
					{
						Name:    "ignoreme0",
						Mac:     "00:12:34:56:78:9a",
						Sandbox: "", // host interface
					},
				},
				IPs: []*cnicurrent.IPConfig{
					// Note that Gateway addresses are not used because
					// there's no routes with nil gateway
					{
						Version: "4",
						Address: net.IPNet{
							IP:   net.IPv4(1, 1, 1, 1),
							Mask: net.CIDRMask(8, 32),
						},
						Gateway:   net.IPv4(1, 2, 3, 4),
						Interface: 0,
					},
					{
						Version: "4",
						Address: net.IPNet{
							IP:   net.IPv4(192, 168, 100, 42),
							Mask: net.CIDRMask(24, 32),
						},
						Gateway:   net.IPv4(192, 168, 100, 1),
						Interface: 1,
					},
				},
				Routes: []*cnitypes.Route{
					{
						Dst: net.IPNet{
							IP:   net.IPv4zero,
							Mask: net.CIDRMask(0, 32),
						},
						GW: net.IPv4(1, 2, 3, 4),
					},
				},
				DNS: cnitypes.DNS{
					Nameservers: []string{"1.2.3.4"},
					Search:      []string{"some", "search"},
				},
			}),
			expectedNetworkConfig: map[string]interface{}{
				"version": float64(1),
				"config": []interface{}{
					map[string]interface{}{
						"mac_address": "00:11:22:33:44:55",
						"name":        "cni0",
						"subnets": []interface{}{
							map[string]interface{}{
								"address":         "1.1.1.1/8",
								"dns_nameservers": []interface{}{"1.2.3.4"},
								"dns_search":      []interface{}{"some", "search"},
								"type":            "static",
							},
						},
						"type": "physical",
					},
					map[string]interface{}{
						"mac_address": "00:11:22:33:ab:cd",
						"name":        "cni1",
						"subnets": []interface{}{
							map[string]interface{}{
								"address":         "192.168.100.42/24",
								"dns_nameservers": []interface{}{"1.2.3.4"},
								"dns_search":      []interface{}{"some", "search"},
								"type":            "static",
							},
						},
						"type": "physical",
					},
					map[string]interface{}{
						"destination": "0.0.0.0/0",
						"gateway":     "1.2.3.4",
						"type":        "route",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// we're not invoking actual iso generation here so "/foobar"
			// as isoDir will do
			g := NewCloudInitGenerator(tc.config, tc.volumeMap, "/foobar")

			if tc.expectedMetaData != nil {
				metaDataBytes, err := g.generateMetaData()
				if err != nil {
					t.Fatalf("generateMetaData(): %v", err)
				}
				var metaData map[string]interface{}
				if err := json.Unmarshal(metaDataBytes, &metaData); err != nil {
					t.Fatalf("Can't unmarshal meta-data: %v", err)
				}

				if !reflect.DeepEqual(tc.expectedMetaData, metaData) {
					t.Errorf("Bad meta-data:\n%s\nUnmarshaled:\n%s", metaDataBytes, spew.Sdump(metaData))
				}
			}

			userDataBytes, err := g.generateUserData()
			if err != nil {
				t.Fatalf("generateUserData(): %v", err)
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

			if tc.expectedNetworkConfig != nil {
				networkConfigBytes, err := g.generateNetworkConfiguration()
				if err != nil {
					t.Fatalf("generateNetworkConfiguration(): %v", err)
				}
				var networkConfig map[string]interface{}
				if err := yaml.Unmarshal(networkConfigBytes, &networkConfig); err != nil {
					t.Fatalf("Can't unmarshal user-data: %v", err)
				}
				if !reflect.DeepEqual(tc.expectedNetworkConfig, networkConfig) {
					t.Errorf("Bad network-config:\n%s\nUnmarshaled:\n%s", networkConfigBytes, spew.Sdump(networkConfig))
				}
			}
		})
	}
}

func TestGenerateDisk(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "nocloud-")
	if err != nil {
		t.Fatalf("Can't create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewCloudInitGenerator(&VMConfig{
		PodName:           "foo",
		PodNamespace:      "default",
		ParsedAnnotations: &VirtletAnnotations{},
	}, nil, tmpDir)
	diskDef, err := g.GenerateDisk()
	if err != nil {
		t.Fatalf("GenerateDisk(): %v", err)
	}
	if !reflect.DeepEqual(diskDef, &libvirtxml.DomainDisk{
		Type:     "file",
		Device:   "cdrom",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: g.IsoPath()},
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	}) {
		t.Errorf("Bad disk definition:\n%s", spew.Sdump(diskDef))
	}
	m, err := testutils.IsoToMap(g.IsoPath())
	if err != nil {
		t.Fatalf("IsoToMap(): %v", err)
	}

	if !reflect.DeepEqual(m, map[string]interface{}{
		"meta-data":      "{\"instance-id\":\"foo.default\",\"local-hostname\":\"foo\"}",
		"network-config": "version: 1\n",
		"user-data":      "#cloud-config\n",
	}) {
		t.Errorf("Bad iso content:\n%s", spew.Sdump(m))
	}
}

func TestEnvDataGeneration(t *testing.T) {
	expected := "key=value\n"
	g := NewCloudInitGenerator(&VMConfig{
		Environment: []*VMKeyValue{
			{Key: "key", Value: "value"},
		},
	}, nil, "")

	output := g.generateEnvVarsContent()
	if output != expected {
		t.Errorf("Bad environment data generated:\n%s\nExpected:\n%s", output, expected)
	}
}

func TestAddingSecrets(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("Can't create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	location := filepath.Join(tmpDir, "volumes/kubernetes.io~secret/test-volume")
	if err := os.MkdirAll(location, 0755); err != nil {
		t.Fatalf("Can't create secrets directory in temp dir: %v", err)
	}

	f, err := os.Create(filepath.Join(location, "file"))
	if err != nil {
		t.Fatalf("Can't create sample file in temp directory: %v", err)
	}
	f.Chmod(0640)
	defer f.Close()
	if _, err := f.WriteString("test content"); err != nil {
		t.Fatalf("Error during write to test file: %v", err)
	}

	userData := make(map[string]interface{})
	g := NewWriteFilesManipulator(userData, []*VMMount{
		{ContainerPath: "/container", HostPath: location},
	})

	g.AddSecrets()

	expectedUserData := map[string]interface{}{
		"write_files": []interface{}{
			map[string]interface{}{
				"path":        "/container/file",
				"content":     "dGVzdCBjb250ZW50",
				"encoding":    "b64",
				"permissions": "0640",
			},
		},
	}

	if !reflect.DeepEqual(userData, expectedUserData) {
		t.Errorf("Bad user-data:\n%s\nExpected:\n%s", spew.Sdump(userData), spew.Sdump(expectedUserData))
	}
}

func TestAddingConfigMap(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("Can't create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	location := filepath.Join(tmpDir, "volumes/kubernetes.io~configmap/test-volume")
	if err := os.MkdirAll(location, 0755); err != nil {
		t.Fatal("Can't create secrets directory in temp dir: %v", err)
	}

	f, err := os.Create(filepath.Join(location, "file"))
	if err != nil {
		t.Fatal("Can't create sample file in temp directory: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString("test content"); err != nil {
		t.Fatal("Error during write to test file: %v", err)
	}

	userData := make(map[string]interface{})
	g := NewWriteFilesManipulator(userData, []*VMMount{
		{ContainerPath: "/container", HostPath: location},
	})

	g.AddConfigMapEntries()

	expectedUserData := map[string]interface{}{
		"write_files": []interface{}{
			map[string]interface{}{
				"path":        "/container/file",
				"content":     "dGVzdCBjb250ZW50",
				"encoding":    "b64",
				"permissions": "0644",
			},
		},
	}

	if !reflect.DeepEqual(userData, expectedUserData) {
		t.Errorf("Bad user-data:\n%s\nExpected:\n%s", spew.Sdump(userData), spew.Sdump(expectedUserData))
	}
}

func TestAddingFileLikeMount(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("Can't create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fname := filepath.Join(tmpDir, "file")
	f, err := os.Create(fname)
	if err != nil {
		t.Fatal("Can't create sample file in temp directory: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString("test content"); err != nil {
		t.Fatal("Error during write to test file: %v", err)
	}

	userData := make(map[string]interface{})
	g := NewWriteFilesManipulator(userData, []*VMMount{
		{ContainerPath: "/container", HostPath: fname},
	})

	g.AddFileLikeMounts()

	expectedUserData := map[string]interface{}{
		"write_files": []interface{}{
			map[string]interface{}{
				"path":        "/container",
				"content":     "dGVzdCBjb250ZW50",
				"encoding":    "b64",
				"permissions": "0644",
			},
		},
	}

	if !reflect.DeepEqual(userData, expectedUserData) {
		t.Errorf("Bad user-data:\n%s\nExpected:\n%s", spew.Sdump(userData), spew.Sdump(expectedUserData))
	}
}
