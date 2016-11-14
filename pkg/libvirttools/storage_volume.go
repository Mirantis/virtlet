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
#include <libvirt/libvirt.h>
#include <libvirt/virterror.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"github.com/golang/glog"
	"unsafe"
)

const (
	defaultCapacity     = 1024
	defaultCapacityUnit = "MB"
)

type StorageBackend interface {
	GenerateVolXML(pool C.virStoragePoolPtr, shortName string, capacity int, capacityUnit, libvirtFilepath string) string
	CreateVol(pool C.virStoragePoolPtr, volName string, capacity int, capacityUnit string) (C.virStorageVolPtr, error)
}

func GetStorageBackend(name string) (StorageBackend, error) {
	switch name {
	case poolTypeDir:
		return &LocalFilesystemBackend{}, nil
	case poolTypeRBD:
		return &RBDBackend{}, nil
	}
	return nil, fmt.Errorf("there is no such a storage backend: %s", name)
}

type LocalFilesystemBackend struct{}

func (LocalFilesystemBackend) GenerateVolXML(pool C.virStoragePoolPtr, shortName string, capacity int, capacityUnit string, path string) string {
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

type RBDBackend struct{}

func (RBDBackend) GenerateVolXML(pool C.virStoragePoolPtr, shortName string, capacity int, capacityUnit, libvirtFilepath string) string {
	return ""
}

func (LocalFilesystemBackend) CreateVol(pool C.virStoragePoolPtr, volName string, capacity int, capacityUnit string) (C.virStorageVolPtr, error) {
	volXML := `
<volume>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
</volume>`
	volXML = fmt.Sprintf(volXML, volName, capacityUnit, capacity)
	glog.V(2).Infof("Create volume using XML description: %s", volXML)
	cVolXML := C.CString(volXML)
	defer C.free(unsafe.Pointer(cVolXML))
	vol := C.virStorageVolCreateXML(pool, cVolXML, 0)
	if vol == nil {
		return nil, GetLastError()
	}
	return vol, nil
}

func (RBDBackend) CreateVol(pool C.virStoragePoolPtr, volName string, capacity int, capacityUnit string) (C.virStorageVolPtr, error) {
	return nil, nil
}

func VolGetInfo(vol C.virStorageVolPtr) (C.virStorageVolInfoPtr, error) {
	var volInfo C.virStorageVolInfo
	if status := C.virStorageVolGetInfo(vol, &volInfo); status != 0 {
		return nil, GetLastError()
	}
	return &volInfo, nil
}

func VolGetPath(vol C.virStorageVolPtr) (string, error) {
	cPath := C.virStorageVolGetPath(vol)
	if cPath == nil {
		return "", GetLastError()
	}
	return C.GoString(cPath), nil

}

func LookupVol(name string, pool C.virStoragePoolPtr) (C.virStorageVolPtr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	vol := C.virStorageVolLookupByName(pool, cName)
	if vol == nil {
		return nil, GetLastError()
	}
	return vol, nil
}

func RemoveVol(name string, pool C.virStoragePoolPtr) error {
	vol, err := LookupVol(name, pool)
	if err != nil {
		return err
	}
	if status := C.virStorageVolDelete(vol, 0); status != 0 {
		return GetLastError()
	}
	return nil
}
