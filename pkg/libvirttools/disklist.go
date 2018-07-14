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

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type diskItem struct {
	driver diskDriver
	volume VMVolume
}

func (di *diskItem) setup(config *types.VMConfig) (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	diskDef, fsDef, err := di.volume.Setup()
	if err != nil {
		return nil, nil, err
	}
	if diskDef != nil {
		diskDef.Target = di.driver.target()
		diskDef.Address = di.driver.address()
	}
	return diskDef, fsDef, nil
}

type diskList struct {
	config *types.VMConfig
	items  []*diskItem
}

// newDiskList creates a diskList for the specified types.VMConfig, volume
// source and volume owner
func newDiskList(config *types.VMConfig, source VMVolumeSource, owner volumeOwner) (*diskList, error) {
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

// setup performs the setup procedure on each volume in the diskList
// and returns a list of libvirtxml DomainDisk and domainFileSystems structs
func (dl *diskList) setup() ([]libvirtxml.DomainDisk, []libvirtxml.DomainFilesystem, error) {
	var domainDisks []libvirtxml.DomainDisk
	var domainFileSystems []libvirtxml.DomainFilesystem
	for n, item := range dl.items {
		diskDef, fsDef, err := item.setup(dl.config)
		if err != nil {
			// try to tear down volumes that were already set up
			for _, item := range dl.items[:n] {
				if err := item.volume.Teardown(); err != nil {
					glog.Warningf("Failed to tear down a volume on error: %v", err)
				}
			}
			return nil, nil, err
		}
		if diskDef != nil {
			domainDisks = append(domainDisks, *diskDef)
		}
		if fsDef != nil {
			domainFileSystems = append(domainFileSystems, *fsDef)
		}
	}
	return domainDisks, domainFileSystems, nil
}

// writeImages writes images for volumes that are based on generated
// images (such as cloud-init nocloud datasource).  It must be passed
// a Domain for which images are being generated.
func (dl *diskList) writeImages(domain virt.Domain) error {
	domainDesc, err := domain.XML()
	if err != nil {
		return fmt.Errorf("couldn't get domain xml: %v", err)
	}

	volumeMap := make(diskPathMap)
	for _, item := range dl.items {
		uuid := item.volume.UUID()
		if uuid != "" {
			diskPath, err := item.driver.diskPath(domainDesc)
			if err != nil {
				return err
			}
			volumeMap[uuid] = *diskPath
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
