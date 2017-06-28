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

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt"
)

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
