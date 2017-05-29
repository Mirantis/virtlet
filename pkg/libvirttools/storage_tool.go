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
	"os"
	"path/filepath"
	"strings"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/diskimage"
	"github.com/Mirantis/virtlet/pkg/virt"
)

const (
	poolTypeDir = "dir"
)

type PoolDesc struct {
	volumesDir string
	poolType   string
}

type PoolSet map[string]*PoolDesc

var supportedStoragePools PoolSet = PoolSet{
	"default": &PoolDesc{volumesDir: "/var/lib/libvirt/images", poolType: poolTypeDir},
	"volumes": &PoolDesc{volumesDir: "/var/lib/virtlet", poolType: poolTypeDir},
}

func ensureStoragePool(conn virt.VirtStorageConnection, name string) (virt.VirtStoragePool, error) {
	poolInfo, found := supportedStoragePools[name]
	if !found {
		return nil, fmt.Errorf("pool with name '%s' is unknown", name)
	}

	pool, err := conn.LookupStoragePoolByName(name)
	if err == nil {
		return pool, nil
	}
	return conn.CreateStoragePool(&libvirtxml.StoragePool{
		Type:   poolInfo.poolType,
		Name:   name,
		Target: &libvirtxml.StoragePoolTarget{Path: poolInfo.volumesDir},
	})
}

type StorageTool struct {
	name       string
	rawDevices []string
	pool       virt.VirtStoragePool
	formatDisk func(path string) error
}

func NewStorageTool(conn virt.VirtStorageConnection, poolName, rawDevices string) (*StorageTool, error) {
	pool, err := ensureStoragePool(conn, poolName)
	if err != nil {
		return nil, err
	}
	return &StorageTool{
		name:       poolName,
		pool:       pool,
		rawDevices: strings.Split(rawDevices, ","),
		formatDisk: diskimage.FormatDisk,
	}, nil
}

func (s *StorageTool) SetFormatDisk(formatDisk func(path string) error) {
	s.formatDisk = formatDisk
}

func (s *StorageTool) CreateQCOW2Volume(name string, capacity uint64, capacityUnit string) (virt.VirtStorageVolume, error) {
	return s.pool.CreateStorageVol(&libvirtxml.StorageVolume{
		Name:       name,
		Allocation: &libvirtxml.StorageVolumeSize{Value: 0},
		Capacity:   &libvirtxml.StorageVolumeSize{Unit: capacityUnit, Value: capacity},
		Target:     &libvirtxml.StorageVolumeTarget{Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"}},
	})
}

func (s *StorageTool) CloneVolume(name string, from virt.VirtStorageVolume) (virt.VirtStorageVolume, error) {
	return s.pool.CreateStorageVolClone(&libvirtxml.StorageVolume{
		Name:   name,
		Type:   "file",
		Target: &libvirtxml.StorageVolumeTarget{Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"}},
	}, from)
}

func (s *StorageTool) LookupVolume(name string) (virt.VirtStorageVolume, error) {
	return s.pool.LookupVolumeByName(name)
}

func (s *StorageTool) RemoveVolume(name string) error {
	return s.pool.RemoveVolumeByName(name)
}

func (s *StorageTool) ListVolumes() ([]virt.VirtStorageVolume, error) {
	return s.pool.ListAllVolumes()
}

func (s *StorageTool) CleanAttachedQCOW2Volumes(volumes []*VirtletVolume, containerId string) error {
	for _, virtletVol := range volumes {
		if virtletVol.Format != "qcow2" {
			continue
		}
		volName := containerId + "-" + virtletVol.Name

		err := s.RemoveVolume(volName)
		lastLibvirtErr, ok := err.(libvirt.Error)
		if !ok {
			return errors.New("Failed to cast error to libvirt.Error type")
		}
		if lastLibvirtErr.Code != libvirt.ERR_NO_STORAGE_VOL {
			return fmt.Errorf("error during removal of volume '%s' for container %s: %v", volName, containerId, err)
		}
	}

	return nil
}

// PrepareVolumesToBeAttached returns a list of xml definitions for dom xml of created or raw disks
// letterInd contains the number of drive letters already used by flexvolumes
func (s *StorageTool) PrepareVolumesToBeAttached(volumes []*VirtletVolume, containerId string, letterInd int) ([]libvirtxml.DomainDisk, error) {
	var disks []libvirtxml.DomainDisk

	for i, virtletVol := range volumes {
		if letterInd+i == len(diskLetters) {
			return nil, fmt.Errorf("too much volumes, limit %d of them exceeded on volume '%s'", len(diskLetters), virtletVol.Name)
		}

		volName := containerId + "-" + virtletVol.Name
		virtDev := "vd" + diskLetters[letterInd+i]
		disk := libvirtxml.DomainDisk{
			Device: "disk",
			Source: &libvirtxml.DomainDiskSource{},
			Driver: &libvirtxml.DomainDiskDriver{Name: "qemu"},
			Target: &libvirtxml.DomainDiskTarget{Dev: virtDev, Bus: "virtio"},
		}

		switch virtletVol.Format {
		case "qcow2":
			vol, err := s.CreateQCOW2Volume(volName, uint64(virtletVol.Capacity), virtletVol.CapacityUnit)
			if err != nil {
				return nil, fmt.Errorf("error during creation of volume '%s' with virtlet description %s: %v", volName, virtletVol.Name, err)
			}

			path, err := vol.Path()
			if err != nil {
				return nil, err
			}

			err = s.formatDisk(path)
			if err != nil {
				return nil, err
			}

			disk.Type = "file"
			disk.Driver.Type = "qcow2"
			disk.Source.File = path

		case "rawDevice":
			if err := s.isRawDeviceOnWhitelist(virtletVol.Path); err != nil {
				return nil, err
			}
			if err := verifyRawDeviceAccess(virtletVol.Path); err != nil {
				return nil, err
			}
			disk.Type = "block"
			disk.Driver.Type = "raw"
			disk.Source.Device = virtletVol.Path
		}

		disks = append(disks, disk)
	}

	return disks, nil
}

func (s *StorageTool) isRawDeviceOnWhitelist(path string) error {
	for _, deviceTemplate := range s.rawDevices {
		devicePaths, err := filepath.Glob("/dev/" + deviceTemplate)
		if err != nil {
			return fmt.Errorf("error in raw device whitelist glob pattern '%s': %v", deviceTemplate, err)
		}
		for _, devicePath := range devicePaths {
			if path == devicePath {
				return nil
			}
		}
	}
	return fmt.Errorf("device '%s' not whitelisted on this virtlet node", path)
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

func (s *StorageTool) FileToVolume(path, volumeName string) (virt.VirtStorageVolume, error) {
	imageSize, err := getFileSize(path)
	if err != nil {
		return nil, err
	}
	libvirtFilePath := fmt.Sprintf("/var/lib/libvirt/images/%s", volumeName)
	return s.pool.ImageToVolume(&libvirtxml.StorageVolume{
		Name:       volumeName,
		Allocation: &libvirtxml.StorageVolumeSize{Value: 0},
		Capacity:   &libvirtxml.StorageVolumeSize{Unit: "b", Value: imageSize},
		Target:     &libvirtxml.StorageVolumeTarget{Path: libvirtFilePath},
	}, path)
}

func getFileSize(path string) (uint64, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return uint64(fileInfo.Size()), nil
}
