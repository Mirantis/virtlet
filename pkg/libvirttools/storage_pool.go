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
	"os"
	"path/filepath"
	"strings"

	"github.com/Mirantis/virtlet/pkg/diskimage"
	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

const (
	poolTypeDir = "dir"
)

type Volume struct {
	tool   StorageOperations
	Name   string
	volume *libvirt.StorageVol
}

func (v *Volume) Remove() error {
	return v.tool.RemoveVolume(v.volume)
}

func (v *Volume) GetPath() (string, error) {
	return v.tool.VolumeGetPath(v.volume)
}

type VolumeInfo struct {
	tool StorageOperations
	Name string
	Size uint64
}

func (v *Volume) Info() (*VolumeInfo, error) {
	return volumeInfo(v.tool, v.Name, v.volume)
}

func volumeInfo(tool StorageOperations, name string, volume *libvirt.StorageVol) (*VolumeInfo, error) {
	volInfo, err := tool.VolumeGetInfo(volume)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{Name: name, Size: volInfo.Capacity}, nil
}

type Pool struct {
	tool       StorageOperations
	pool       *libvirt.StoragePool
	volumesDir string
	poolType   string
}

type PoolSet map[string]*Pool

var DefaultPools PoolSet = PoolSet{
	"default": &Pool{volumesDir: "/var/lib/libvirt/images", poolType: poolTypeDir},
	"volumes": &Pool{volumesDir: "/var/lib/virtlet", poolType: poolTypeDir},
}

func createPool(tool StorageOperations, name string, path string, poolType string) (*Pool, error) {
	storagePool := libvirtxml.StoragePool{
		Type:   poolType,
		Name:   name,
		Target: &libvirtxml.StoragePoolTarget{Path: path},
	}

	poolXML, err := storagePool.Marshal()
	if err != nil {
		return nil, err
	}

	glog.V(2).Infof("Creating storage pool (name: %s, path: %s)", name, path)
	pool, err := tool.CreateFromXML(poolXML)
	if err != nil {
		return nil, err
	}
	return &Pool{tool: tool, pool: pool, volumesDir: path, poolType: poolType}, nil
}

func LookupStoragePool(tool StorageOperations, name string) (*Pool, error) {
	poolInfo, exist := DefaultPools[name]
	if !exist {
		return nil, fmt.Errorf("pool with name '%s' is unknown", name)
	}

	pool, _ := tool.LookupByName(name)
	if pool == nil {
		return createPool(tool, name, poolInfo.volumesDir, poolInfo.poolType)
	}
	// TODO: reset libvirt error

	return &Pool{tool: tool, pool: pool, volumesDir: poolInfo.volumesDir, poolType: poolInfo.poolType}, nil
}

func (p *Pool) RemoveVolume(name string) error {
	vol, err := p.LookupVolume(name)
	if err != nil {
		return err
	}
	return vol.Remove()
}

func (p *Pool) CreateVolume(name, volXML string) (*Volume, error) {
	vol, err := p.tool.CreateVolFromXML(p.pool, volXML)
	if err != nil {
		return nil, err
	}
	return &Volume{tool: p.tool, Name: name, volume: vol}, nil
}

func (p *Pool) LookupVolume(name string) (*Volume, error) {
	vol, err := p.tool.LookupVolumeByName(p.pool, name)
	if err != nil {
		return nil, err
	}
	return &Volume{tool: p.tool, Name: name, volume: vol}, nil
}

func (p *Pool) CloneVolume(name, volXML string, from *Volume) (*Volume, error) {
	vol, err := p.tool.CreateVolCloneFromXML(p.pool, volXML, from.volume)
	if err != nil {
		return nil, err
	}
	return &Volume{tool: p.tool, Name: name, volume: vol}, nil
}

func (p *Pool) ListVolumes() ([]*VolumeInfo, error) {
	volumes, err := p.tool.ListAllVolumes(p.pool)
	if err != nil {
		return nil, err
	}

	volumeInfos := make([]*VolumeInfo, 0, len(volumes))

	for _, volume := range volumes {
		name, err := p.tool.VolumeGetName(&volume)
		volInfo, err := volumeInfo(p.tool, name, &volume)
		if err != nil {
			return nil, err
		}

		volumeInfos = append(volumeInfos, volInfo)
	}

	return volumeInfos, nil
}

type StorageTool struct {
	name       string
	tool       StorageOperations
	rawDevices []string
	pool       *Pool
}

func NewStorageTool(conn *libvirt.Connect, poolName, rawDevices string) (*StorageTool, error) {
	tool := NewLibvirtStorageOperations(conn)
	pool, err := LookupStoragePool(tool, poolName)
	if err != nil {
		return nil, err
	}
	return &StorageTool{name: poolName, tool: tool, pool: pool, rawDevices: strings.Split(rawDevices, ",")}, nil
}

func (s *StorageTool) CreateQCOW2Volume(name string, capacity uint64, capacityUnit string) (*Volume, error) {
	volume := libvirtxml.StorageVolume{
		Name:       name,
		Allocation: &libvirtxml.StorageVolumeSize{Value: 0},
		Capacity:   &libvirtxml.StorageVolumeSize{Unit: capacityUnit, Value: capacity},
	}
	volumeXML, err := volume.Marshal()
	if err != nil {
		return nil, err
	}

	glog.V(2).Infof("Create volume using XML description: %s", volumeXML)
	return s.pool.CreateVolume(name, volumeXML)
}

func (s *StorageTool) CloneVolume(name string, from *Volume) (*Volume, error) {
	volume := libvirtxml.StorageVolume{
		Name:   name,
		Type:   "file",
		Target: &libvirtxml.StorageVolumeTarget{Format: &libvirtxml.StorageVolumeTargetFormat{Type: "qcow2"}},
	}
	cloneXML, err := volume.Marshal()
	if err != nil {
		return nil, err
	}

	glog.V(2).Infof("Creating volume clone with name '%s' from volume '%s'.", name, from.Name)
	return s.pool.CloneVolume(name, cloneXML, from)
}

func (s *StorageTool) LookupVolume(name string) (*Volume, error) {
	return s.pool.LookupVolume(name)
}

func (s *StorageTool) RemoveVolume(name string) error {
	return s.pool.RemoveVolume(name)
}

func (s *StorageTool) ListVolumes() ([]*VolumeInfo, error) {
	return s.pool.ListVolumes()
}

func (s *StorageTool) CleanAttachedQCOW2Volumes(volumes []*VirtletVolume, containerId string) error {
	for _, virtletVol := range volumes {
		if virtletVol.Format != "qcow2" {
			continue
		}
		volName := containerId + "-" + virtletVol.Name
		if err := s.RemoveVolume(volName); err != nil {
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

			path, err := vol.GetPath()
			if err != nil {
				return nil, err
			}

			err = diskimage.FormatDisk(path)
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

func (s *StorageTool) PullFileToVolume(path, volumeName string) error {
	imageSize, err := getFileSize(path)
	if err != nil {
		return err
	}
	libvirtFilePath := fmt.Sprintf("/var/lib/libvirt/images/%s", volumeName)

	volume := libvirtxml.StorageVolume{
		Name:       volumeName,
		Allocation: &libvirtxml.StorageVolumeSize{Value: 0},
		Capacity:   &libvirtxml.StorageVolumeSize{Unit: "B", Value: imageSize},
		Target:     &libvirtxml.StorageVolumeTarget{Path: libvirtFilePath},
	}
	volXML, err := volume.Marshal()
	if err != nil {
		return err
	}

	return s.tool.PullImageToVolume(s.pool.pool, volumeName, path, volXML)
}

func getFileSize(path string) (uint64, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return uint64(fileInfo.Size()), nil
}
