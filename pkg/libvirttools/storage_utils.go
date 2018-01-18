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
	"fmt"
	"os"
	"runtime"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt"
)

// diskPath contains paths that can be used to locate a device inside
// the VM using Linux-specific /dev or /sys paths
type diskPath struct {
	// devPath denotes path to the device under /dev, e.g.
	// /dev/disk/by-path/virtio-pci-0000:00:03.0-scsi-0:0:0:0 or
	// /dev/disk/by-path/pci-0000:00:03.0-virtio-pci-0000:01:01.0
	devPath string
	// sysfsPath denotes a path to a directory in sysfs that
	// contains a file with the same name as the device in /dev, e.g.
	// /sys/devices/pci0000:00/0000:00:03.0/0000:01:01.0/virtio*/block/ or
	// /sys/devices/pci0000:00/0000:00:03.0/virtio*/host*/target*:0:0/*:0:0:0/block/sda
	// (note that in the latter case * is used instead of host because host number appear
	// to be wrong in sysfs for some reason)
	// The path needs to be globbed and the single file name from there
	// should be used as device name, e.g.
	// ls -l /dev/`ls /sys/devices/pci0000:00/0000:00:03.0/0000:01:01.0/virtio*/block/`
	sysfsPath string
}

// diskPathMap maps volume uuids to diskPath items
type diskPathMap map[string]diskPath

var supportedStoragePools = map[string]string{
	"default": "/var/lib/libvirt/images",
	"volumes": "/var/lib/virtlet/volumes",
}

func ensureStoragePool(conn virt.VirtStorageConnection, name string) (virt.VirtStoragePool, error) {
	poolDir, found := supportedStoragePools[name]
	if !found {
		return nil, fmt.Errorf("pool with name '%s' is unknown", name)
	}

	pool, err := conn.LookupStoragePoolByName(name)
	if err == nil {
		return pool, nil
	}
	return conn.CreateStoragePool(&libvirtxml.StoragePool{
		Type:   "dir",
		Name:   name,
		Target: &libvirtxml.StoragePoolTarget{Path: poolDir},
	})
}

func verifyRawDeviceAccess(path string) error {
	// XXX: make tests pass on non-Linux systems
	if runtime.GOOS != "linux" {
		return nil
	}

	// TODO: verify access rights for qemu process to this path
	pathInfo, err := os.Stat(path)
	if err != nil {
		return err
	}

	// is this device and not char device?
	if pathInfo.Mode()&os.ModeDevice != 0 && pathInfo.Mode()&os.ModeCharDevice == 0 {
		return nil
	}

	return fmt.Errorf("path '%s' points to something other than block device", path)
}
