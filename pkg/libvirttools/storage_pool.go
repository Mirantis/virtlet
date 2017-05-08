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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mirantis/virtlet/pkg/diskimage"
	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
)

const (
	defaultCapacity     = 1024
	defaultCapacityUnit = "MB"
	poolTypeDir         = "dir"
	diskXMLTemplate     = `
<disk type='file' device='disk'>
    <driver name='qemu' type='raw'/>
    <source file='%s'/>
    <target dev='%s' bus='virtio'/>
</disk>`
)

type Volume struct {
	tool   StorageOperations
	Name   string
	volume *libvirt.StorageVol
}

type VirtletVolume struct {
	Name         string `json:"Name"`
	Format       string `json:"Format"`
	Capacity     int    `json:"Capacity,string,omitempty"`
	CapacityUnit string `json:"CapacityUnit,omitempty"`
	Path         string `json:"Path,omitempty"`
}

func (vol *VirtletVolume) UnmarshalJSON(data []byte) error {
	// volAlias is needed to prevent recursive calls to UnmarshalJSON
	type volAlias VirtletVolume
	volWithDefaults := &volAlias{
		Format: "qcow2",
	}

	err := json.Unmarshal(data, volWithDefaults)

	if err != nil {
		return err
	}

	if volWithDefaults.Format == "qcow2" {
		if volWithDefaults.Capacity == 0 {
			volWithDefaults.Capacity = defaultCapacity
		}
		if volWithDefaults.CapacityUnit == "" {
			volWithDefaults.CapacityUnit = defaultCapacityUnit
		}
	}

	*vol = VirtletVolume(*volWithDefaults)
	if err := vol.validate(); err != nil {
		return fmt.Errorf("validation failed for volumes definition within pod's annotations: %s", err)
	}

	return nil
}

func (vol *VirtletVolume) validate() error {
	if vol.Name == "" {
		return errors.New("volume name is mandatory")
	}

	switch vol.Format {
	case "qcow2":
		if vol.Path != "" {
			return fmt.Errorf("qcow2 volume should not have Path but it has it set to: %s", vol.Path)
		}
	case "rawDevice":
		if vol.Capacity != 0 {
			return fmt.Errorf("raw volume should not have Capacity, but it has it equal to: %d", vol.Capacity)
		}
		if vol.CapacityUnit != "" {
			return fmt.Errorf("raw volume should not have CapacityUnit, but it has it set to: %s", vol.CapacityUnit)
		}
		if !strings.HasPrefix(vol.Path, "/dev/") {
			return fmt.Errorf("raw volume Path needs to be prefixed by '/dev/', but it's whole value is: ", vol.Path)
		}
		if err := verifyRawDeviceAccess(vol.Path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported volume format: %s", vol.Format)
	}

	return nil
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

func generatePoolXML(name string, path string, poolType string) string {
	poolXML := `
<pool type="%s">
    <name>%s</name>
    <target>
	<path>%s</path>
    </target>
</pool>`
	return fmt.Sprintf(poolXML, poolType, name, path)
}

func createPool(tool StorageOperations, name string, path string, poolType string) (*Pool, error) {
	poolXML := generatePoolXML(name, path, poolType)

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

func (s *StorageTool) GenerateVolumeXML(shortName string, capacity uint64, capacityUnit string, path string) string {
	volXML := `
<volume>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
    <target>
        <path>%s</path>
    </target>
</volume>`
	return fmt.Sprintf(volXML, shortName, capacityUnit, capacity, path)
}

func (s *StorageTool) CreateQCOW2Volume(name string, capacity uint64, capacityUnit string) (*Volume, error) {
	volumeXML := `
<volume>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
</volume>`
	volumeXML = fmt.Sprintf(volumeXML, name, capacityUnit, capacity)
	glog.V(2).Infof("Create volume using XML description: %s", volumeXML)
	return s.pool.CreateVolume(name, volumeXML)
}

func (s *StorageTool) CreateVolumeClone(name string, from *Volume) (*Volume, error) {
	cloneXMLtemplate := `
<volume type='file'>
    <name>%s</name>
    <target>
         <format type='qcow2'/>
    </target>
</volume>`
	cloneXML := fmt.Sprintf(cloneXMLtemplate, name)
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

func (s *StorageTool) CleanAttachedQCOW2Volumes(virtletVolsDesc string, containerId string) error {

	var vols []VirtletVolume
	if err := json.Unmarshal([]byte(virtletVolsDesc), &vols); err != nil {
		return fmt.Errorf("error when unmarshalling json string with volumes description '%s' for container %s: %v", virtletVolsDesc, containerId, err)
	}

	for _, virtletVol := range vols {
		if err := virtletVol.validate(); err != nil {
			return err
		}
		if virtletVol.Format == "qcow2" {
			volName := containerId + "-" + virtletVol.Name
			if err := s.RemoveVolume(volName); err != nil {
				return fmt.Errorf("error during removal of volume '%s' for container %s: %v", volName, containerId, err)
			}
		}
	}

	return nil
}

// PrepareVolumesToBeAttached returns a list of xml definitions for dom xml of created or raw disks
// letterInd contains the number of drive letters already used by flexvolumes
func (s *StorageTool) PrepareVolumesToBeAttached(virtletVolsDesc string, containerId string, letterInd int) ([]string, error) {
	if virtletVolsDesc == "" {
		return nil, nil
	}

	var disksXMLs []string
	var virtletVols []VirtletVolume
	if err := json.Unmarshal([]byte(virtletVolsDesc), &virtletVols); err != nil {
		return nil, fmt.Errorf("error when unmarshalling json string with volumes description '%s' for container %s: %v", virtletVolsDesc, containerId, err)
	}

	for i, virtletVol := range virtletVols {
		if err := virtletVol.validate(); err != nil {
			return nil, fmt.Errorf("volume '%s' have an error in definition: %s", virtletVol.Name, err)
		}
		if letterInd+i == len(diskLetters) {
			return nil, fmt.Errorf("too much volumes, limit %d of them exceeded on volume '%s'", len(diskLetters), virtletVol.Name)
		}

		volName := containerId + "-" + virtletVol.Name
		virtDev := "vd" + diskLetters[letterInd+i]
		var diskXML string
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

			diskXML = fmt.Sprintf(diskXMLTemplate, path, virtDev)

		case "rawDevice":
			if err := s.isRawDeviceOnWhitelist(virtletVol.Path); err != nil {
				return nil, err
			}
			diskXML = generateRawDeviceXML(virtletVol.Path, virtDev)
		}

		disksXMLs = append(disksXMLs, diskXML)
	}

	return disksXMLs, nil
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

const (
	rawDeviceTemplateXML = `<disk type='block' device='disk'>
  <driver name='qemu' type='raw'/>
  <source dev='%s'/>
  <target dev='%s' bus='virtio'/>
</disk>
`
)

func generateRawDeviceXML(path, device string) string {
	return fmt.Sprintf(rawDeviceTemplateXML, path, device)
}

func (s *StorageTool) PullImageToVolume(path, volumeName string) error {
	imageSize, err := getFileSize(path)
	if err != nil {
		return err
	}
	libvirtFilePath := fmt.Sprintf("/var/lib/libvirt/images/%s", volumeName)
	volXML := s.GenerateVolumeXML(volumeName, imageSize, "B", libvirtFilePath)

	return s.tool.PullImageToVolume(s.pool.pool, volumeName, path, volXML)
}

func getFileSize(path string) (uint64, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return uint64(fileInfo.Size()), nil
}
