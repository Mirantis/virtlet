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
	"testing"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

func TestDiskPath(t *testing.T) {
	for _, tc := range []struct {
		name       string
		driverName diskDriverName
		diskCount  int
		devList    libvirtxml.DomainDeviceList
		diskPaths  []diskPath
	}{
		{
			name:       "scsi driver",
			driverName: diskDriverScsi,
			diskCount:  3,
			devList: libvirtxml.DomainDeviceList{
				Disks: []libvirtxml.DomainDisk{
					{
						Device: "disk",
						// NOTE: Source & Driver aren't used here, so omitting them for the sake of simplicity
						Target: &libvirtxml.DomainDiskTarget{
							Dev: "sda",
							Bus: "scsi",
						},
						Address: scsiAddress(0, 0, 0, 0),
					},
					{
						Device: "disk",
						Target: &libvirtxml.DomainDiskTarget{
							Dev: "sdb",
							Bus: "scsi",
						},
						Address: scsiAddress(0, 0, 0, 1),
					},
					{
						Device: "cdrom",
						Target: &libvirtxml.DomainDiskTarget{
							// this one is usually sr0 in the VM,
							// but it doesn't matter here
							Dev: "sdc",
							Bus: "scsi",
						},
						Address:  scsiAddress(0, 0, 0, 2),
						ReadOnly: &libvirtxml.DomainDiskReadOnly{},
					},
				},
				Controllers: []libvirtxml.DomainController{
					{
						Type:  "pci",
						Model: "pci-root",
					},
					{
						Type:    "scsi",
						Index:   puint(0),
						Model:   "virtio-scsi",
						Address: pciAddress(0, 0, 3, 0),
					},
				},
			},
			diskPaths: []diskPath{
				{
					"/dev/disk/by-path/virtio-pci-0000:00:03.0-scsi-0:0:0:0",
					"/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:0/block/",
				},
				{
					"/dev/disk/by-path/virtio-pci-0000:00:03.0-scsi-0:0:0:1",
					"/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:1/block/",
				},
				{
					"/dev/disk/by-path/virtio-pci-0000:00:03.0-scsi-0:0:0:2",
					"/sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:2/block/",
				},
			},
		},
		{
			name:       "virtio driver",
			driverName: diskDriverVirtio,
			diskCount:  3,
			devList: libvirtxml.DomainDeviceList{
				Disks: []libvirtxml.DomainDisk{
					{
						Device: "disk",
						Target: &libvirtxml.DomainDiskTarget{
							Dev: "vda",
							Bus: "virtio",
						},
						Address: pciAddress(0, 1, 1, 0),
					},
					{
						Device: "disk",
						Target: &libvirtxml.DomainDiskTarget{
							Dev: "vdb",
							Bus: "virtio",
						},
						Address: pciAddress(0, 1, 2, 0),
					},
					{
						Device: "cdrom",
						Target: &libvirtxml.DomainDiskTarget{
							Dev: "vdc",
							Bus: "virtio",
						},
						Address:  pciAddress(0, 1, 3, 0),
						ReadOnly: &libvirtxml.DomainDiskReadOnly{},
					},
				},
				Controllers: []libvirtxml.DomainController{
					{
						Type:  "pci",
						Model: "pci-root",
					},
					{
						Type:    "pci",
						Index:   puint(1),
						Model:   "pci-bridge",
						Address: pciAddress(0, 0, 3, 0),
					},
				},
			},
			diskPaths: []diskPath{
				{
					"/dev/disk/by-path/pci-0000:00:03.0-virtio-pci-0000:01:01.0",
					"/sys/devices/pci0000:00/0000:00:03.0/0000:01:01.0/virtio*/block/",
				},
				{
					"/dev/disk/by-path/pci-0000:00:03.0-virtio-pci-0000:01:02.0",
					"/sys/devices/pci0000:00/0000:00:03.0/0000:01:02.0/virtio*/block/",
				},
				{
					"/dev/disk/by-path/pci-0000:00:03.0-virtio-pci-0000:01:03.0",
					"/sys/devices/pci0000:00/0000:00:03.0/0000:01:03.0/virtio*/block/",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory, err := getDiskDriverFactory(tc.driverName)
			if err != nil {
				t.Fatalf("creating factory %q: %v", tc.driverName, err)
			}
			domain := &libvirtxml.Domain{Devices: &tc.devList}
			for n := 0; n < tc.diskCount; n++ {
				driver, err := factory(n)
				if err != nil {
					t.Errorf("error making driver #%d: %v", n, err)
					continue
				}
				diskPath, err := driver.diskPath(domain)
				if err != nil {
					t.Errorf("diskPath() #%d: %v", n, err)
					continue
				}
				if diskPath.devPath != tc.diskPaths[n].devPath {
					t.Errorf("bad devPath #%d: expected %q, got %q", n, tc.diskPaths[n].devPath, diskPath.devPath)
				}
				if diskPath.sysfsPath != tc.diskPaths[n].sysfsPath {
					t.Errorf("bad sysfsPath #%d: expected %q, got %q", n, tc.diskPaths[n].sysfsPath, diskPath.sysfsPath)
				}
			}
		})
	}
}

func puint(n uint) *uint { return &n }

func scsiAddress(controller, bus, target, unit uint) *libvirtxml.DomainAddress {
	return &libvirtxml.DomainAddress{
		Drive: &libvirtxml.DomainAddressDrive{
			Controller: &controller,
			Bus:        &bus,
			Target:     &target,
			Unit:       &unit,
		},
	}
}

func pciAddress(domain, bus, slot, function uint) *libvirtxml.DomainAddress {
	return &libvirtxml.DomainAddress{
		PCI: &libvirtxml.DomainAddressPCI{
			Domain:   &domain,
			Bus:      &bus,
			Slot:     &slot,
			Function: &function,
		},
	}
}

// TODO: sort pci-bridge controllers by index !!!
