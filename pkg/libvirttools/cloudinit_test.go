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

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/tests/gm"
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
	if err := utils.WriteJSON(infoPath, info, 0777); err != nil {
		t.Fatalf("WriteJSON(): %q: %v", infoPath, err)
	}
	return &fakeFlexvolume{
		uuid: uuid,
		part: part,
		path: volDir,
	}
}

func buildNetworkedPodConfig(cniResult *cnicurrent.Result, imageTypeName string) *types.VMConfig {
	var descs []*network.InterfaceDescription
	for _, iface := range cniResult.Interfaces {
		if iface.Sandbox != "" {
			mac, _ := net.ParseMAC(iface.Mac)
			descs = append(descs, &network.InterfaceDescription{
				HardwareAddr: mac,
				MTU:          1500,
			})
		}
	}
	return &types.VMConfig{
		PodName:           "foo",
		PodNamespace:      "default",
		ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageType(imageTypeName)},
		ContainerSideNetwork: &network.ContainerSideNetwork{
			Result:     cniResult,
			Interfaces: descs,
		},
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
	volDevs := []types.VMVolumeDevice{
		{
			DevicePath: "/dev/disk-a",
			HostPath:   vols[0].path,
		},
		{
			DevicePath: "/dev/disk-b",
			HostPath:   vols[1].path,
		},
		{
			DevicePath: "/dev/disk-c",
			HostPath:   vols[2].path,
		},
	}

	sharedDir := filepath.Join(tmpDir, "640ad329-e533-4ec0-820f-f11b2255bd56")
	if err := os.MkdirAll(sharedDir, 0777); err != nil {
		t.Fatalf("MkdirAll(): %q: %v", sharedDir, err)
	}

	for _, tc := range []struct {
		name                string
		config              *types.VMConfig
		volumeMap           diskPathMap
		verifyMetaData      bool
		verifyUserData      bool
		verifyNetworkConfig bool
		verifyUserDataStr   bool
	}{
		{
			name: "plain pod",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
			},
			verifyMetaData:      true,
			verifyNetworkConfig: true,
		},
		{
			name: "metadata for configdrive",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeConfigDrive},
			},
			verifyMetaData: true,
		},
		{
			name: "pod with ssh keys",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
					SSHKeys:     []string{"key1", "key2"},
					CDImageType: types.CloudInitImageTypeNoCloud,
				},
			},
			verifyMetaData: true,
		},
		{
			name: "pod with ssh keys and meta-data override",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
					SSHKeys: []string{"key1", "key2"},
					MetaData: map[string]interface{}{
						"instance-id": "foobar",
					},
					CDImageType: types.CloudInitImageTypeNoCloud,
				},
			},
			verifyMetaData: true,
		},
		{
			name: "pod with user data",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
					UserData: map[string]interface{}{
						"users": []interface{}{
							map[string]interface{}{
								"name": "cloudy",
							},
						},
					},
					SSHKeys:     []string{"key1", "key2"},
					CDImageType: types.CloudInitImageTypeNoCloud,
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
		},
		{
			name: "pod with env variables",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
				Environment: []types.VMKeyValue{
					{"foo", "bar"},
					{"baz", "abc"},
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
		},
		{
			name: "pod with env variables and user data",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
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
					CDImageType: types.CloudInitImageTypeNoCloud,
				},
				Environment: []types.VMKeyValue{
					{"foo", "bar"},
					{"baz", "abc"},
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
		},
		{
			name: "pod with user data script",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
					UserDataScript: "#!/bin/sh\necho hi\n",
					SSHKeys:        []string{"key1", "key2"},
					CDImageType:    types.CloudInitImageTypeNoCloud,
				},
			},
			verifyMetaData:    true,
			verifyUserDataStr: true,
		},
		{
			name: "pod with volumes to mount",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
				Mounts: []types.VMMount{
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
			volumeMap: diskPathMap{
				vols[0].uuid: {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:1",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:1/block/",
				},
				vols[1].uuid: {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:2",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:2/block/",
				},
				vols[2].uuid: {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:3",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:3/block/",
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
		},
		{
			name: "9pfs volume",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
				Mounts: []types.VMMount{
					{
						ContainerPath: "/opt",
						HostPath:      sharedDir,
					},
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
		},
		{
			name: "pod with volume devices",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
				VolumeDevices:     volDevs,
			},
			volumeMap: diskPathMap{
				volDevs[0].UUID(): {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:1",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:1/block/",
				},
				volDevs[1].UUID(): {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:2",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:2/block/",
				},
				volDevs[2].UUID(): {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:3",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:3/block/",
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
		},
		{
			name: "pod with volume devices and volumes to mount",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
					CDImageType: types.CloudInitImageTypeNoCloud,
					UserData: map[string]interface{}{
						"mounts": []interface{}{
							[]interface{}{"/dev/foo1", "/foo1"},
							[]interface{}{"/dev/disk-a", "/foobar"},
							[]interface{}{"/dev/disk-b", "/foobar"},
						},
					},
				},
				VolumeDevices: volDevs[:2],
				Mounts: []types.VMMount{
					{
						ContainerPath: "/var/lib/foobar",
						HostPath:      vols[2].path,
					},
				},
			},
			volumeMap: diskPathMap{
				volDevs[0].UUID(): {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:1",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:1/block/",
				},
				volDevs[1].UUID(): {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:2",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:2/block/",
				},
				vols[2].uuid: {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:3",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:3/block/",
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
		},
		{
			name: "pod with persistent rootfs",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
				VolumeDevices: []types.VMVolumeDevice{
					{
						DevicePath: "/",
						HostPath:   volDevs[0].HostPath,
					},
				},
			},
			verifyMetaData: true,
			verifyUserData: true,
			// make sure network config is null for the persistent rootfs case
			verifyNetworkConfig: true,
		},
		{
			name: "pod with forced dhcp network config",
			config: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{ForceDHCPNetworkConfig: true},
			},
			verifyMetaData: true,
			verifyUserData: true,
			// make sure network config is null
			verifyNetworkConfig: true,
		},
		{
			name: "injecting mount script into user data script",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
					UserDataScript: "#!/bin/sh\necho hi\n@virtlet-mount-script@",
					CDImageType:    types.CloudInitImageTypeNoCloud,
				},
				Mounts: []types.VMMount{
					{
						ContainerPath: "/opt",
						HostPath:      vols[0].path,
					},
				},
			},
			volumeMap: diskPathMap{
				vols[0].uuid: {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:1",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:1/block/",
				},
			},
			verifyMetaData:    true,
			verifyUserDataStr: true,
		},
		{
			name: "injecting mount and symlink scripts into user data script",
			config: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				ParsedAnnotations: &types.VirtletAnnotations{
					UserDataScript: "#!/bin/sh\necho hi\n@virtlet-mount-script@",
					CDImageType:    types.CloudInitImageTypeNoCloud,
				},
				VolumeDevices: volDevs[1:2],
				Mounts: []types.VMMount{
					{
						ContainerPath: "/opt",
						HostPath:      vols[0].path,
					},
				},
			},
			volumeMap: diskPathMap{
				vols[0].uuid: {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:1",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:1/block/",
				},
				volDevs[1].UUID(): {
					devPath:   "/dev/disk/by-path/virtio-pci-0000:00:01.0-scsi-0:0:0:2",
					sysfsPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:2/block/",
				},
			},
			verifyMetaData:    true,
			verifyUserDataStr: true,
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
			}, "nocloud"),
			verifyNetworkConfig: true,
		},
		// FIXME: it's not possible to produce link-scoped routes
		// through cloud-init, so this may need more work
		// {
		// 	name: "pod with calico network config",
		// 	config: buildNetworkedPodConfig(&cnicurrent.Result{
		// 		Interfaces: []*cnicurrent.Interface{
		// 			{
		// 				Name:    "cni0",
		// 				Mac:     "00:11:22:33:44:55",
		// 				Sandbox: "/var/run/netns/bae464f1-6ee7-4ee2-826e-33293a9de95e",
		// 			},
		// 		},
		// 		IPs: []*cnicurrent.IPConfig{
		// 			{
		// 				Version: "4",
		// 				Address: net.IPNet{
		// 					IP:   net.IPv4(192, 168, 135, 136),
		// 					Mask: net.CIDRMask(32, 32),
		// 				},
		// 				Gateway:   net.IPv4(169, 254, 1, 1),
		// 				Interface: 0,
		// 			},
		// 		},
		// 		Routes: []*cnitypes.Route{
		// 			// link-scoped route
		// 			{
		// 				Dst: net.IPNet{
		// 					IP:   net.IPv4(168, 254, 1, 1),
		// 					Mask: net.CIDRMask(32, 32),
		// 				},
		// 				GW: net.IPv4zero,
		// 			},
		// 			// default route
		// 			{
		// 				Dst: net.IPNet{
		// 					IP:   net.IPv4zero,
		// 					Mask: net.CIDRMask(0, 32),
		// 				},
		// 				GW: net.IPv4(168, 254, 1, 1),
		// 			},
		// 		},
		// 		DNS: cnitypes.DNS{
		// 			Nameservers: []string{"1.2.3.4"},
		// 			Search:      []string{"some", "search"},
		// 		},
		// 	}, "nocloud"),
		// 	verifyNetworkConfig: true,
		// },
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
					// additional route like in flannel case
					{
						Dst: net.IPNet{
							IP:   net.IPv4(1, 2, 0, 0),
							Mask: net.CIDRMask(16, 32),
						},
						GW: net.IPv4(1, 2, 3, 4),
					},
				},
				DNS: cnitypes.DNS{
					Nameservers: []string{"1.2.3.4"},
					Search:      []string{"some", "search"},
				},
			}, "nocloud"),
			verifyNetworkConfig: true,
		},
		{
			name: "pod with network config - configdrive",
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
			}, "configdrive"),
			verifyNetworkConfig: true,
		},
		{
			name: "pod with multiple network interfaces - configdrive",
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
			}, "configdrive"),
			verifyNetworkConfig: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// we're not invoking actual iso generation here so "/foobar"
			// as isoDir will do
			g := NewCloudInitGenerator(tc.config, "/foobar")

			r := map[string]interface{}{}
			if tc.verifyMetaData {
				metaDataBytes, err := g.generateMetaData()
				if err != nil {
					t.Fatalf("generateMetaData(): %v", err)
				}
				var metaData map[string]interface{}
				if err := json.Unmarshal(metaDataBytes, &metaData); err != nil {
					t.Fatalf("Can't unmarshal meta-data: %v", err)
				}

				r["meta-data"] = metaData
			}

			userDataBytes, err := g.generateUserData(tc.volumeMap)
			if err != nil {
				t.Fatalf("generateUserData(): %v", err)
			}

			if tc.verifyUserDataStr {
				r["user-data-str"] = string(userDataBytes)
			}

			if tc.verifyUserData {
				if !bytes.HasPrefix(userDataBytes, []byte("#cloud-config\n")) {
					t.Errorf("No #cloud-config header")
				}
				var userData map[string]interface{}
				if err := yaml.Unmarshal(userDataBytes, &userData); err != nil {
					t.Fatalf("Can't unmarshal user-data: %v", err)
				}

				r["user-data"] = userData
			}

			if tc.verifyNetworkConfig {
				networkConfigBytes, err := g.generateNetworkConfiguration()
				if err != nil {
					t.Fatalf("generateNetworkConfiguration(): %v", err)
				}
				var networkConfig map[string]interface{}
				if err := yaml.Unmarshal(networkConfigBytes, &networkConfig); err != nil {
					t.Fatalf("Can't unmarshal user-data: %v", err)
				}
				r["network-config"] = networkConfig
			}
			gm.Verify(t, gm.NewYamlVerifier(r))
		})
	}
}

func TestCloudInitDiskDef(t *testing.T) {
	g := NewCloudInitGenerator(&types.VMConfig{
		PodName:           "foo",
		PodNamespace:      "default",
		ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
	}, "")
	diskDef := g.DiskDef()
	if !reflect.DeepEqual(diskDef, &libvirtxml.DomainDisk{
		Device:   "cdrom",
		Driver:   &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
		Source:   &libvirtxml.DomainDiskSource{File: &libvirtxml.DomainDiskSourceFile{File: g.IsoPath()}},
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	}) {
		t.Errorf("Bad disk definition:\n%s", spew.Sdump(diskDef))
	}
}

func TestCloudInitGenerateImage(t *testing.T) {
	for _, tc := range []struct {
		name          string
		vmConfig      *types.VMConfig
		expectedFiles map[string]interface{}
	}{
		{
			name: "nocloud",
			vmConfig: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
			},
			expectedFiles: map[string]interface{}{
				"meta-data":      "{\"instance-id\":\"foo.default\",\"local-hostname\":\"foo\"}",
				"network-config": "version: 1\n",
				"user-data":      "#cloud-config\n",
			},
		},
		{
			name: "nocloud with persistent rootfs",
			vmConfig: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				VolumeDevices: []types.VMVolumeDevice{
					{
						DevicePath: "/",
						HostPath:   "/dev/loop0",
					},
				},
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeNoCloud},
			},
			expectedFiles: map[string]interface{}{
				"meta-data": "{\"instance-id\":\"foo.default\",\"local-hostname\":\"foo\"}",
				"user-data": "#cloud-config\n",
			},
		},
		{
			name: "configdrive",
			vmConfig: &types.VMConfig{
				PodName:           "foo",
				PodNamespace:      "default",
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeConfigDrive},
			},
			expectedFiles: map[string]interface{}{
				"openstack": map[string]interface{}{
					"latest": map[string]interface{}{
						"meta_data.json":    "{\"hostname\":\"foo\",\"instance-id\":\"foo.default\",\"local-hostname\":\"foo\",\"uuid\":\"foo.default\"}",
						"network_data.json": "{}",
						"user_data":         "#cloud-config\n",
					},
				},
			},
		},
		{
			name: "configdrive with persistent rootfs",
			vmConfig: &types.VMConfig{
				PodName:      "foo",
				PodNamespace: "default",
				VolumeDevices: []types.VMVolumeDevice{
					{
						DevicePath: "/",
						HostPath:   "/dev/loop0",
					},
				},
				ParsedAnnotations: &types.VirtletAnnotations{CDImageType: types.CloudInitImageTypeConfigDrive},
			},
			expectedFiles: map[string]interface{}{
				"openstack": map[string]interface{}{
					"latest": map[string]interface{}{
						"meta_data.json": "{\"hostname\":\"foo\",\"instance-id\":\"foo.default\",\"local-hostname\":\"foo\",\"uuid\":\"foo.default\"}",
						"user_data":      "#cloud-config\n",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir, err := ioutil.TempDir("", "config-")
			if err != nil {
				t.Fatalf("Can't create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			g := NewCloudInitGenerator(tc.vmConfig, tmpDir)
			if err := g.GenerateImage(nil); err != nil {
				t.Fatalf("GenerateImage(): %v", err)
			}

			m, err := testutils.IsoToMap(g.IsoPath())
			if err != nil {
				t.Fatalf("IsoToMap(): %v", err)
			}

			if !reflect.DeepEqual(m, tc.expectedFiles) {
				t.Errorf("Bad iso content:\n%s", spew.Sdump(m))
			}
		})
	}
}

func TestEnvDataGeneration(t *testing.T) {
	expected := "key=value\n"
	g := NewCloudInitGenerator(&types.VMConfig{
		Environment: []types.VMKeyValue{
			{Key: "key", Value: "value"},
		},
	}, "")

	output := g.generateEnvVarsContent()
	if output != expected {
		t.Errorf("Bad environment data generated:\n%s\nExpected:\n%s", output, expected)
	}
}

func verifyWriteFiles(t *testing.T, u *writeFilesUpdater, expectedWriteFiles ...interface{}) {
	userData := make(map[string]interface{})
	u.updateUserData(userData)
	expectedUserData := map[string]interface{}{"write_files": expectedWriteFiles}
	if !reflect.DeepEqual(userData, expectedUserData) {
		t.Errorf("Bad user-data:\n%s\nExpected:\n%s", spew.Sdump(userData), spew.Sdump(expectedUserData))
	}
}

func withFakeVolumeDir(t *testing.T, subdir string, perms os.FileMode, toRun func(location string)) {
	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("Can't create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var location, filePath string
	if subdir != "" {
		location = filepath.Join(tmpDir, subdir)
		if err := os.MkdirAll(location, 0755); err != nil {
			t.Fatalf("Can't create secrets directory in temp dir: %v", err)
		}
		filePath = filepath.Join(location, "file")
	} else {
		filePath = filepath.Join(tmpDir, "file")
		location = filePath
	}

	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Can't create sample file in temp directory: %v", err)
	}
	if _, err := f.WriteString("test content"); err != nil {
		f.Close()
		t.Fatalf("Error writing test file: %v", err)
	}
	if perms != 0 {
		if err := f.Chmod(perms); err != nil {
			t.Fatalf("Chmod(): %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Error closing test file: %v", err)
	}

	toRun(location)
}

func TestAddingSecrets(t *testing.T) {
	withFakeVolumeDir(t, "volumes/kubernetes.io~secret/test-volume", 0640, func(location string) {
		u := newWriteFilesUpdater([]types.VMMount{
			{ContainerPath: "/container", HostPath: location},
		})
		u.addSecrets()
		verifyWriteFiles(t, u, map[string]interface{}{
			"path":        "/container/file",
			"content":     "dGVzdCBjb250ZW50",
			"encoding":    "b64",
			"permissions": "0640",
		})
	})
}

func TestAddingConfigMap(t *testing.T) {
	withFakeVolumeDir(t, "volumes/kubernetes.io~configmap/test-volume", 0, func(location string) {
		u := newWriteFilesUpdater([]types.VMMount{
			{ContainerPath: "/container", HostPath: location},
		})
		u.addConfigMapEntries()
		verifyWriteFiles(t, u, map[string]interface{}{
			"path":        "/container/file",
			"content":     "dGVzdCBjb250ZW50",
			"encoding":    "b64",
			"permissions": "0644",
		})
	})
}

func TestAddingFileLikeMount(t *testing.T) {
	withFakeVolumeDir(t, "", 0, func(location string) {
		u := newWriteFilesUpdater([]types.VMMount{
			{ContainerPath: "/container", HostPath: location},
		})
		u.addFileLikeMounts()
		verifyWriteFiles(t, u, map[string]interface{}{
			"path":        "/container",
			"content":     "dGVzdCBjb250ZW50",
			"encoding":    "b64",
			"permissions": "0644",
		})
	})
}

func TestMtuForMacAddress(t *testing.T) {
	interfaces := []*network.InterfaceDescription{
		{
			MTU:          1234,
			HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0xa, 0xb},
		},
	}

	for _, tc := range []struct {
		mac             string
		shouldHaveError bool
		value           uint16
	}{
		{
			mac:             "00:00:00:00:0a:0b",
			shouldHaveError: false,
			value:           1234,
		},
		{
			mac:             "00:00:00:00:0A:0B",
			shouldHaveError: false,
			value:           1234,
		},
		{
			mac:             "00:00:00:0a:0b:0c",
			shouldHaveError: true,
			value:           0,
		},
	} {
		value, err := mtuForMacAddress(tc.mac, interfaces)
		if err == nil && tc.shouldHaveError {
			t.Errorf("Missing expected error")
		}
		if err != nil && !tc.shouldHaveError {
			t.Errorf("Received unexpected error: %v", err)
		}
		if value != tc.value {
			t.Errorf("Received value %q is diffrent from expected %q", value, tc.value)
		}
	}
}
