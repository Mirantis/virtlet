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
	"fmt"
	"strings"

	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt"
)

type diskItem struct {
	driver diskDriver
	volume VMVolume
}

func (di *diskItem) setup(config *VMConfig) (*libvirtxml.DomainDisk, error) {
	diskDef, err := di.volume.Setup()
	if err != nil {
		return nil, err
	}
	diskDef.Target = di.driver.target()
	diskDef.Address = di.driver.address()
	return diskDef, nil
}

type diskList struct {
	config *VMConfig
	items  []*diskItem
}

// newDiskSet creates a diskList for the specified VMConfig, volume
// source and volume owner
func newDiskList(config *VMConfig, source VMVolumeSource, owner VolumeOwner) (*diskList, error) {
	vmVols, err := source(config, owner)
	if err != nil {
		return nil, err
	}

	diskDriverFactory, err := getDiskDriverFactory(config.ParsedAnnotations.DiskDriver)
	if err != nil {
		return nil, err
	}
	var items []*diskItem
	for n, volume := range vmVols {
		driver, err := diskDriverFactory(n)
		if err != nil {
			return nil, err
		}
		items = append(items, &diskItem{driver, volume})
	}

	return &diskList{config, items}, nil
}

// setupVolumes performs the setup procedure on each volume in the
// diskList and returns a list of libvirtxml DomainDisk structs
func (dl *diskList) setup() ([]libvirtxml.DomainDisk, error) {
	var domainDisks []libvirtxml.DomainDisk
	for n, item := range dl.items {
		diskDef, err := item.setup(dl.config)
		if err != nil {
			// try to tear down volumes that were already set up
			for _, item := range dl.items[:n] {
				if err := item.volume.Teardown(); err != nil {
					glog.Warningf("Failed to tear down a volume on error: %v", err)
				}
			}
			return nil, err
		}
		domainDisks = append(domainDisks, *diskDef)
	}
	return domainDisks, nil
}

// writeVolumeImages writes images for volumes that are based on
// generated images (such as cloud-init nocloud datasource).  It must
// be passed a VirtDomain for which images are being generated.
func (dl *diskList) writeImages(domain virt.VirtDomain) error {
	domainDesc, err := domain.Xml()
	if err != nil {
		return fmt.Errorf("couldn't get domain xml: %v", err)
	}

	volumeMap := make(map[string]string)
	for _, item := range dl.items {
		uuid := item.volume.Uuid()
		if uuid != "" {
			diskPath, err := item.driver.diskPath(domainDesc)
			if err != nil {
				return err
			}
			volumeMap[uuid] = diskPath.devPath
		}
	}

	for _, item := range dl.items {
		if err := item.volume.WriteImage(volumeMap); err != nil {
			return err
		}
	}

	return nil
}

func (dl *diskList) teardown() error {
	var errs []string
	for _, item := range dl.items {
		if err := item.volume.Teardown(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if errs != nil {
		return fmt.Errorf("failed to tear down some of the volumes:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}
