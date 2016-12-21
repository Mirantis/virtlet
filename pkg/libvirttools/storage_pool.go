/*
Copyright 2016 Mirantis

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

/*
#include <fcntl.h>
#include <libvirt/libvirt.h>
#include <libvirt/virterror.h>
#include <stdlib.h>
#include <unistd.h>
#include "image.h"
*/
import "C"

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/golang/glog"
)

const (
	defaultCapacity     = 1024
	defaultCapacityUnit = "MB"
	poolTypeDir         = "dir"
)

type Volume struct {
	Name   string
	volume C.virStorageVolPtr
}

func (v *Volume) Remove() error {
	if status := C.virStorageVolDelete(v.volume, 0); status != 0 {
		return GetLibvirtLastError()
	}
	return nil
}

func (v *Volume) GetPath() (string, error) {
	cPath := C.virStorageVolGetPath(v.volume)
	if cPath == nil {
		return "", GetLibvirtLastError()
	}
	return C.GoString(cPath), nil
}

type VolumeInfo struct {
	Name string
	Size uint64
}

func (v *Volume) Info() (*VolumeInfo, error) {
	return volumeInfo(v.Name, v.volume)
}

func volumeInfo(name string, volume C.virStorageVolPtr) (*VolumeInfo, error) {
	var volInfo C.virStorageVolInfo
	if status := C.virStorageVolGetInfo(volume, &volInfo); status != 0 {
		return nil, GetLibvirtLastError()
	}
	return &VolumeInfo{Name: name, Size: uint64(volInfo.capacity)}, nil
}

type Pool struct {
	pool       C.virStoragePoolPtr
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

func createPool(conn C.virConnectPtr, name string, path string, poolType string) (*Pool, error) {
	poolXML := generatePoolXML(name, path, poolType)
	bPoolXML := []byte(poolXML)
	cPoolXML := (*C.char)(unsafe.Pointer(&bPoolXML[0]))

	glog.V(2).Infof("Creating storage pool (name: %s, path: %s)", name, path)
	var pool C.virStoragePoolPtr
	if pool = C.virStoragePoolCreateXML(conn, cPoolXML, 0); pool == nil {
		return nil, GetLibvirtLastError()
	}
	return &Pool{pool: pool, volumesDir: path, poolType: poolType}, nil
}

func LookupStoragePool(conn C.virConnectPtr, name string) (*Pool, error) {
	poolInfo, exist := DefaultPools[name]
	if !exist {
		return nil, fmt.Errorf("pool with name '%s' is unknown", name)
	}

	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	storagePool := C.virStoragePoolLookupByName(conn, cName)
	if storagePool == nil {
		return createPool(conn, name, poolInfo.volumesDir, poolInfo.poolType)
	}
	// TODO: reset libvirt error

	return &Pool{pool: storagePool, volumesDir: poolInfo.volumesDir, poolType: poolInfo.poolType}, nil
}

func (p *Pool) RemoveVolume(name string) error {
	vol, err := p.LookupVolume(name)
	if err != nil {
		return err
	}
	return vol.Remove()
}

func (p *Pool) CreateVolume(name, volXML string) (*Volume, error) {
	cVolXML := C.CString(volXML)
	defer C.free(unsafe.Pointer(cVolXML))
	vol := C.virStorageVolCreateXML(p.pool, cVolXML, 0)
	if vol == nil {
		return nil, GetLibvirtLastError()
	}
	return &Volume{Name: name, volume: vol}, nil
}

func (p *Pool) LookupVolume(name string) (*Volume, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	vol := C.virStorageVolLookupByName(p.pool, cName)
	if vol == nil {
		return nil, GetLibvirtLastError()
	}
	return &Volume{Name: name, volume: vol}, nil
}

func (p *Pool) ListVolumes() ([]*VolumeInfo, error) {
	var cList *C.virStorageVolPtr
	count := C.virStoragePoolListAllVolumes(p.pool, (**C.virStorageVolPtr)(&cList), 0)
	if count < 0 {
		return nil, GetLibvirtLastError()
	}
	header := reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(cList)),
		Len:  int(count),
		Cap:  int(count),
	}
	cVolumes := *(*[]C.virStorageVolPtr)(unsafe.Pointer(&header))

	volumeInfos := make([]*VolumeInfo, 0, count)

	for _, cVolume := range cVolumes {
		name := C.GoString(C.virStorageVolGetName(cVolume))
		volInfo, err := volumeInfo(name, cVolume)
		if err != nil {
			return nil, err
		}

		volumeInfos = append(volumeInfos, volInfo)
	}

	return volumeInfos, nil
}

type StorageTool struct {
	name string
	conn C.virConnectPtr
	pool *Pool
}

func NewStorageTool(conn C.virConnectPtr, poolName string) (*StorageTool, error) {
	pool, err := LookupStoragePool(conn, poolName)
	if err != nil {
		return nil, err
	}
	return &StorageTool{name: poolName, conn: conn, pool: pool}, nil
}

func (s *StorageTool) GenerateVolumeXML(shortName string, capacity int, capacityUnit string, path string) string {
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

func (s *StorageTool) CreateVolume(name string, capacity int, capacityUnit string) (*Volume, error) {
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

func (s *StorageTool) CreateSnapshot(name string, capacity int, capacityUnit string, backingStorePath string) (*Volume, error) {
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

func (s *StorageTool) PullImageToVolume(path, volumeName string) error {
	libvirtFilePath := fmt.Sprintf("/var/lib/libvirt/images/%s", volumeName)
	volXML := s.GenerateVolumeXML(volumeName, 5, "G", libvirtFilePath)

	cShortName := C.CString(volumeName)
	defer C.free(unsafe.Pointer(cShortName))
	cFilepath := C.CString(path)
	defer C.free(unsafe.Pointer(cFilepath))
	cVolXML := C.CString(volXML)
	defer C.free(unsafe.Pointer(cVolXML))

	status := C.pullImage(s.conn, s.pool.pool, cShortName, cFilepath, cVolXML)
	return cErrorHandler.Convert(status)
}
