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
	"fmt"
	"os"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
)

const (
	defaultCapacity     = 1024
	defaultCapacityUnit = "MB"
	poolTypeDir         = "dir"
)

type Volume struct {
	tool   StorageOperations
	Name   string
	volume *libvirt.StorageVol
}

type VirtletVolume struct {
	Name         string `json:"Name"`
	Format       string `json:"Format"`
	Capacity     int    `json:"Capacity,string"`
	CapacityUnit string `json:"CapacityUnit"`
}

func (vol *VirtletVolume) UnmarshalJSON(data []byte) error {
	// volAlias is needed to prevent recursive calls to UnmarshalJSON
	type volAlias VirtletVolume
	volWithDefaults := &volAlias{
		Format:       "qcow2",
		Capacity:     defaultCapacity,
		CapacityUnit: defaultCapacityUnit,
	}

	err := json.Unmarshal(data, volWithDefaults)

	if err == nil && volWithDefaults.Name == "" {
		return fmt.Errorf("Validation failed for volumes definition within pod's annotations: volume name is mandatory.")
	}

	*vol = VirtletVolume(*volWithDefaults)
	return err
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
	name string
	tool StorageOperations
	pool *Pool
}

func NewStorageTool(conn *libvirt.Connect, poolName string) (*StorageTool, error) {
	tool := NewLibvirtStorageOperations(conn)
	pool, err := LookupStoragePool(tool, poolName)
	if err != nil {
		return nil, err
	}
	return &StorageTool{name: poolName, tool: tool, pool: pool}, nil
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

func (s *StorageTool) CreateVolume(name string, capacity uint64, capacityUnit string) (*Volume, error) {
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

func (s *StorageTool) CreateSnapshot(name string, capacity uint64, capacityUnit string, backingStorePath string) (*Volume, error) {
	snapshotXML := `
<volume type='file'>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
    <target>
         <format type='qcow2'/>
    </target>
    <backingStore>
         <path>%s</path>
         <format type='qcow2'/>
     </backingStore>
</volume>`
	snapshotXML = fmt.Sprintf(snapshotXML, name, capacityUnit, capacity, backingStorePath)
	glog.V(2).Infof("Create volume using XML description: %s", snapshotXML)
	return s.pool.CreateVolume(name, snapshotXML)
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

func (s *StorageTool) CleanAttachedVolumes(virtletVolsDesc string, containerId string) error {

	var vols []VirtletVolume
	if err := json.Unmarshal([]byte(virtletVolsDesc), &vols); err != nil {
		return fmt.Errorf("error when unmarshalling json string with volumes description '%s' for container %s: %v", virtletVolsDesc, containerId, err)
	}

	for _, virtletVol := range vols {
		switch virtletVol.Format {
		case "qcow2":
			volName := containerId + "-" + virtletVol.Name
			if err := s.RemoveVolume(volName); err != nil {
				return fmt.Errorf("error during removal of volume '%s' for container %s: %v", volName, containerId, err)
			}
		default:
			return fmt.Errorf("unsupported volume format '%s' in volume '%s' definition", virtletVol.Format, virtletVol.Name)
		}
	}

	return nil
}

func (s *StorageTool) CreateVolumesToBeAttached(virtletVolsDesc string, containerId string, letterInd int) ([]string, error) {
	if virtletVolsDesc == "" {
		return nil, nil
	}

	var volumesXMLs []string
	var virtletVols []VirtletVolume
	if err := json.Unmarshal([]byte(virtletVolsDesc), &virtletVols); err != nil {
		return nil, fmt.Errorf("error when unmarshalling json string with volumes description '%s' for container %s: %v", virtletVolsDesc, containerId, err)
	}

	for i, virtletVol := range virtletVols {
		if letterInd+i == len(diskLetters) {
			return nil, fmt.Errorf("too much volumes, limit %d of them exceeded on volume '%s'", len(diskLetters), virtletVol.Name)
		}

		volName := containerId + "-" + virtletVol.Name
		virtDev := "vd" + diskLetters[letterInd+i]
		var volXML string
		switch virtletVol.Format {
		case "qcow2":
			vol, err := s.CreateVolume(volName, defaultCapacity, defaultCapacityUnit)
			if err != nil {
				return nil, fmt.Errorf("Error during creation of volume '%s' with virtlet description %s: %v", volName, virtletVol.Name, err)
			}

			path, err := vol.GetPath()
			if err != nil {
				return nil, err
			}

			err = utils.FormatDisk(path)
			if err != nil {
				return nil, err
			}

			volXML = fmt.Sprintf(volXMLTemplate, path, virtDev)

		default:
			return nil, fmt.Errorf("unsupported volume format '%s' in volume '%s' definition", virtletVol.Format, virtletVol.Name)
		}

		volumesXMLs = append(volumesXMLs, volXML)
	}

	return volumesXMLs, nil
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
