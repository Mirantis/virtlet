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
	"github.com/golang/glog"
	"unsafe"
	"fmt"
)

const (
	poolTypeDir = "dir"
	poolTypeRBD = "rbd"
)

type Pool struct {
	volumesPoolDir string
	poolType string
}

type PoolSet map[string]*Pool

var DefaultPools PoolSet = PoolSet{
	"default": &Pool {volumesPoolDir: "/var/lib/libvirt/images", poolType: poolTypeDir},
	"volumes": &Pool {volumesPoolDir: "/var/lib/virtlet", poolType: poolTypeDir},
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

func createPool(conn C.virConnectPtr, name string, path string, poolType string) error {
	poolXML := generatePoolXML(name, path, poolType)
	bPoolXML := []byte(poolXML)
	cPoolXML := (*C.char)(unsafe.Pointer(&bPoolXML[0]))

	glog.Infof("Creating storage pool (name: %s, path: %s)", name, path)
	if pool := C.virStoragePoolCreateXML(conn, cPoolXML, 0); pool == nil {
		return GetLastError()
	}
	return nil
}

func LookupStoragePool(conn C.virConnectPtr, name string) (C.virStoragePoolPtr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	storagePool := C.virStoragePoolLookupByName(conn, cName)
	if storagePool == nil {
		if poolInfo, exist := DefaultPools[name]; exist {
			if err := createPool(conn, name, poolInfo.volumesPoolDir, poolInfo.poolType); err != nil {
				return nil, err
			}
		} else {
			return nil, GetLastError()
		}
	}
	return storagePool, nil
}
