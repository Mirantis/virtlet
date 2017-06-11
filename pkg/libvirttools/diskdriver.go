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

type diskDriverFunc func(n int) (string, *libvirtxml.DomainDiskTarget, *libvirtxml.DomainAddress, error)

var diskDriverMap = map[DiskDriver]diskDriverFunc{
	DiskDriverVirtio: virtioBlkDriver,
	DiskDriverScsi:   scsiDriver,
}

func virtioBlkDriver(n int) (string, *libvirtxml.DomainDiskTarget, *libvirtxml.DomainAddress, error) {
	diskChar := minBlockDevChar + n
	if diskChar > maxVirtioBlockDevChar {
		return "", nil, nil, errors.New("too many virtio block devices")
	}
	virtDev := fmt.Sprintf("vd%c", diskChar)
	diskTarget := &libvirtxml.DomainDiskTarget{
		Dev: virtDev,
		Bus: "virtio",
	}
	domain := uint(0)
	// use bus1 to have more predictable addressing for virtio devs
	bus := uint(1)
	slot := uint(n + 1)
	function := uint(0)
	diskAddress := &libvirtxml.DomainAddress{
		Type:     "pci",
		Domain:   &domain,
		Bus:      &bus,
		Slot:     &slot,
		Function: &function,
	}
	return "/dev/" + virtDev, diskTarget, diskAddress, nil
}

func scsiDriver(n int) (string, *libvirtxml.DomainDiskTarget, *libvirtxml.DomainAddress, error) {
	diskChar := minBlockDevChar + n
	if diskChar > maxScsiBlockDevChar {
		return "", nil, nil, errors.New("too many scsi block devices")
	}
	virtDev := fmt.Sprintf("sd%c", diskChar)
	diskTarget := &libvirtxml.DomainDiskTarget{
		Dev: virtDev,
		Bus: "scsi",
	}
	controller := uint(0)
	bus := uint(0)
	target := uint(0)
	unit := uint(n)
	diskAddress := &libvirtxml.DomainAddress{
		Type:       "drive",
		Controller: &controller,
		Bus:        &bus,
		Target:     &target,
		Unit:       &unit,
	}
	// FIXME: in case of cdrom, that's actually sr0, but we're
	// only using cdrom for nocloud drive currently
	return "/dev/" + virtDev, diskTarget, diskAddress, nil
}

func getDiskDriverFunc(name DiskDriver) (diskDriverFunc, error) {
	if f, found := diskDriverMap[name]; found {
		return f, nil
	}
	return nil, fmt.Errorf("bad disk driver name: %q", name)
}
