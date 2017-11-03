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

	libvirtxml "github.com/libvirt/libvirt-go-xml"
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
	devPath() string
	target() *libvirtxml.DomainDiskTarget
	address() *libvirtxml.DomainAddress
}

type diskDriverFactory func(n int) (diskDriver, error)

var diskDriverMap = map[DiskDriver]diskDriverFactory{
	DiskDriverVirtio: virtioBlkDriverFactory,
	DiskDriverScsi:   scsiDriverFactory,
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

func (d *virtioBlkDriver) devPath() string {
	return fmt.Sprintf("/dev/vd%c", d.diskChar)
}

func (d *virtioBlkDriver) target() *libvirtxml.DomainDiskTarget {
	return &libvirtxml.DomainDiskTarget{
		Dev: fmt.Sprintf("vd%c", d.diskChar),
		Bus: "virtio",
	}
}

func (d *virtioBlkDriver) address() *libvirtxml.DomainAddress {
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

func (d *scsiDriver) devPath() string {
	// FIXME: in case of cdrom, that's actually sr0, but we're
	// only using cdrom for nocloud drive currently
	return fmt.Sprintf("/dev/sd%c", d.diskChar)
}

func (d *scsiDriver) target() *libvirtxml.DomainDiskTarget {
	return &libvirtxml.DomainDiskTarget{
		Dev: fmt.Sprintf("sd%c", d.diskChar),
		Bus: "scsi",
	}
}

func (d *scsiDriver) address() *libvirtxml.DomainAddress {
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

func getDiskDriverFactory(name DiskDriver) (diskDriverFactory, error) {
	if f, found := diskDriverMap[name]; found {
		return f, nil
	}
	return nil, fmt.Errorf("bad disk driver name: %q", name)
}
