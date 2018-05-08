/*
Copyright 2016-2017 Mirantis

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
	"errors"
	"fmt"
	"sort"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

const (
	minBlockDevChar = 'a'
	// https://access.redhat.com/documentation/en-US/Red_Hat_Enterprise_Linux/5/html/Virtualization/sect-Virtualization-Virtualization_limitations-KVM_limitations.html
	// Actually there can be more than 21 block devices (including
	// the root volume), but we want to be on the safe side here
	maxVirtioBlockDevChar = 'u'
	maxScsiBlockDevChar   = 'z'
)

type diskDriver interface {
	diskPath(domainDef *libvirtxml.Domain) (*diskPath, error)
	target() *libvirtxml.DomainDiskTarget
	address() *libvirtxml.DomainAddress
}

type diskDriverFactory func(n int) (diskDriver, error)

var diskDriverMap = map[types.DiskDriverName]diskDriverFactory{
	types.DiskDriverVirtio: virtioBlkDriverFactory,
	types.DiskDriverScsi:   scsiDriverFactory,
}

type virtioBlkDriver struct {
	n        int
	diskChar int
}

func virtioBlkDriverFactory(n int) (diskDriver, error) {
	diskChar := minBlockDevChar + n
	if diskChar > maxVirtioBlockDevChar {
		return nil, errors.New("too many virtio block devices")
	}
	return &virtioBlkDriver{n, diskChar}, nil
}

func (d *virtioBlkDriver) diskPath(domainDef *libvirtxml.Domain) (*diskPath, error) {
	disk, err := findDisk(domainDef, d.devName())
	if err != nil {
		return nil, err
	}
	devPath, sysfsPath, err := pciPath(domainDef, disk.Address)
	if err != nil {
		return nil, err
	}
	return &diskPath{devPath, sysfsPath + "/virtio*/block/"}, nil
}

func (d *virtioBlkDriver) devName() string {
	return fmt.Sprintf("vd%c", d.diskChar)
}

func (d *virtioBlkDriver) target() *libvirtxml.DomainDiskTarget {
	return &libvirtxml.DomainDiskTarget{
		Dev: d.devName(),
		Bus: "virtio",
	}
}

func (d *virtioBlkDriver) address() *libvirtxml.DomainAddress {
	// FIXME: we can let libvirt auto-assign the addresses.
	// We'll have to add auto-assignment logic to fake_domain.go though
	domain := uint(0)
	// use bus1 to have more predictable addressing for virtio devs
	bus := uint(1)
	slot := uint(d.n + 1)
	function := uint(0)
	return &libvirtxml.DomainAddress{
		PCI: &libvirtxml.DomainAddressPCI{
			Domain:   &domain,
			Bus:      &bus,
			Slot:     &slot,
			Function: &function,
		},
	}
}

type scsiDriver struct {
	n        int
	diskChar int
}

func scsiDriverFactory(n int) (diskDriver, error) {
	diskChar := minBlockDevChar + n
	if diskChar > maxScsiBlockDevChar {
		return nil, errors.New("too many scsi block devices")
	}
	return &scsiDriver{n, diskChar}, nil
}

func (d *scsiDriver) diskPath(domainDef *libvirtxml.Domain) (*diskPath, error) {
	scsiControllers := findControllers(domainDef, "scsi")
	switch {
	case len(scsiControllers) == 0:
		return nil, errors.New("no scsi controllers found")
	case len(scsiControllers) > 1:
		// linux kernel reports wrong host number in sysfs for some reason
		return nil, errors.New("more than one scsi controller is not supported")
	}

	disk, err := findDisk(domainDef, d.devName())
	if err != nil {
		return nil, err
	}
	if disk.Address.Drive == nil || disk.Address.Drive.Controller == nil || disk.Address.Drive.Bus == nil || disk.Address.Drive.Target == nil || disk.Address.Drive.Unit == nil {
		return nil, fmt.Errorf("bad disk address for scsi disk %q", d.devName())
	}
	if *disk.Address.Drive.Controller != 0 {
		return nil, fmt.Errorf("bad controller index for scsi disk %q", d.devName())
	}

	devPath, sysfsPath, err := pciPath(domainDef, scsiControllers[0].Address)
	if err != nil {
		return nil, err
	}
	return &diskPath{
		fmt.Sprintf("%s-scsi-0:%d:%d:%d", devPath, *disk.Address.Drive.Bus, *disk.Address.Drive.Target, *disk.Address.Drive.Unit),
		// host number are wrong in sysfs for some reason
		fmt.Sprintf("%s/virtio*/host*/target*:%d:%d/*:%d:%d:%d/block/",
			sysfsPath,
			*disk.Address.Drive.Bus,
			*disk.Address.Drive.Target,
			*disk.Address.Drive.Bus,
			*disk.Address.Drive.Target,
			*disk.Address.Drive.Unit),
	}, nil
}

func (d *scsiDriver) devName() string {
	return fmt.Sprintf("sd%c", d.diskChar)
}

func (d *scsiDriver) target() *libvirtxml.DomainDiskTarget {
	return &libvirtxml.DomainDiskTarget{
		Dev: d.devName(),
		Bus: "scsi",
	}
}

func (d *scsiDriver) address() *libvirtxml.DomainAddress {
	// FIXME: we can let libvirt auto-assign the addresses.
	// We'll have to add auto-assignment logic to fake_domain.go though
	controller := uint(0)
	bus := uint(0)
	target := uint(0)
	unit := uint(d.n)
	return &libvirtxml.DomainAddress{
		Drive: &libvirtxml.DomainAddressDrive{
			Controller: &controller,
			Bus:        &bus,
			Target:     &target,
			Unit:       &unit,
		},
	}
}

func getDiskDriverFactory(name types.DiskDriverName) (diskDriverFactory, error) {
	if f, found := diskDriverMap[name]; found {
		return f, nil
	}
	return nil, fmt.Errorf("bad disk driver name: %q", name)
}

func findDisk(domainDef *libvirtxml.Domain, dev string) (*libvirtxml.DomainDisk, error) {
	if domainDef.Devices != nil {
		for _, d := range domainDef.Devices.Disks {
			if d.Target != nil && d.Target.Dev == dev {
				return &d, nil
			}
		}
	}
	return nil, fmt.Errorf("disk %q not found in the domain", dev)
}

func findControllers(domainDef *libvirtxml.Domain, controllerType string) []libvirtxml.DomainController {
	if domainDef.Devices == nil {
		return nil
	}
	// make an empty slice instead of nil because the effects of
	// calling sort.SliceStable() on nil slice are unspecified
	r := []libvirtxml.DomainController{}
	for _, c := range domainDef.Devices.Controllers {
		if c.Type == controllerType {
			r = append(r, c)
		}
	}
	sort.SliceStable(r, func(i, j int) bool {
		var a, b uint
		if r[i].Index != nil {
			a = *r[i].Index
		}
		if r[j].Index != nil {
			b = *r[j].Index
		}
		return a < b
	})

	return r
}

func pciPath(domainDef *libvirtxml.Domain, address *libvirtxml.DomainAddress) (string, string, error) {
	pciControllers := findControllers(domainDef, "pci")
	devPath := "/dev/disk/by-path/"
	sysfsPath := "/sys/devices"
	var recurse func(*libvirtxml.DomainAddress, string, int) error
	recurse = func(address *libvirtxml.DomainAddress, pathPrefix string, depth int) error {
		if depth > 256 { // 256 is big enough here, we're not expecting that many hops
			return fmt.Errorf("can't make path for device address %#v: loop detected", address)
		}
		if address == nil || address.PCI == nil || address.PCI.Domain == nil || address.PCI.Bus == nil || address.PCI.Slot == nil || address.PCI.Function == nil {
			return fmt.Errorf("can't make path for device address %#v", address)
		}
		if *address.PCI.Bus > uint(len(pciControllers)) {
			return fmt.Errorf("bad PCI bus number: %#v", address)
		}
		ctl := pciControllers[*address.PCI.Bus]
		if ctl.Address != nil && ctl.Address.PCI != nil {
			if err := recurse(ctl.Address, "pci", depth+1); err != nil {
				return err
			}
		} else {
			// pci-root is not mentioned in devPath, but is present in sysfsPath
			sysfsPath += fmt.Sprintf("/pci%04x:%02x", *address.PCI.Domain, *address.PCI.Bus)
		}
		addressStr := fmt.Sprintf("%04x:%02x:%02x.%01x", *address.PCI.Domain, *address.PCI.Bus, *address.PCI.Slot, *address.PCI.Function)
		if devPath[len(devPath)-1] != '/' {
			devPath += "-"
		}
		devPath += pathPrefix + "-" + addressStr
		sysfsPath += "/" + addressStr
		return nil
	}
	if err := recurse(address, "virtio-pci", 0); err != nil {
		return "", "", fmt.Errorf("pciPath for %#v: %v", address, err)
	}
	return devPath, sysfsPath, nil
}
