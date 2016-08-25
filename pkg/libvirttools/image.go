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
#cgo LDFLAGS: -lvirt
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
	"net/url"
	"reflect"
	"strings"
	"syscall"
	"unsafe"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/download"
)

type StorageBackend interface {
	GenerateVolXML(pool C.virStoragePoolPtr, shortName string, capacity int, capacityUnit string) string
}

type LocalFilesystemBackend struct{}

func (LocalFilesystemBackend) GenerateVolXML(pool C.virStoragePoolPtr, shortName string, capacity int, capacityUnit string) string {
	volXML := `
<volume>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
    <target>
        <path>%s/%s.img</path>
    </target>
</volume>`
	return fmt.Sprintf(volXML, shortName, capacityUnit, capacity, "/var/lib/libvirt/images", shortName)
}

type RBDBackend struct{}

func (RBDBackend) GenerateVolXML(pool C.virStoragePoolPtr, shortName string, capacity int, capacityUnit string) string {
	return ""
}

type ImageTool struct {
	conn           C.virConnectPtr
	pool           C.virStoragePoolPtr
	storageBackend StorageBackend
}

func lookupStoragePool(conn C.virConnectPtr, name string) (C.virStoragePoolPtr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	storagePool := C.virStoragePoolLookupByName(conn, cName)
	if storagePool == nil {
		return nil, GetLastError()
	}
	return storagePool, nil
}

func cleanURIImageName(name string) string {
	u, err := url.Parse(name)
	if err != nil {
		return name
	}
	segments := strings.Split(u.Path, "/")
	return segments[len(segments)-1]
}

func getStorageBackend(name string) (StorageBackend, error) {
	switch name {
	case "local":
		return &LocalFilesystemBackend{}, nil
	case "rbd":
		return &RBDBackend{}, nil
	}
	return nil, fmt.Errorf("There is no such a storage backend: %s", name)
}

func NewImageTool(conn C.virConnectPtr, poolName string, storageBackendName string) (*ImageTool, error) {
	pool, err := lookupStoragePool(conn, poolName)
	if err != nil {
		return nil, err
	}
	storageBackend, err := getStorageBackend(storageBackendName)
	if err != nil {
		return nil, err
	}
	return &ImageTool{
		conn:           conn,
		pool:           pool,
		storageBackend: storageBackend,
	}, nil
}

func (i *ImageTool) lookupVol(name string) (C.virStorageVolPtr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	vol := C.virStorageVolLookupByName(i.pool, cName)
	if vol == nil {
		return nil, GetLastError()
	}
	return vol, nil
}

func (i *ImageTool) volGetInfo(vol C.virStorageVolPtr) (C.virStorageVolInfoPtr, error) {
	var volInfo C.virStorageVolInfo
	if status := C.virStorageVolGetInfo(vol, &volInfo); status != 0 {
		return nil, GetLastError()
	}
	return &volInfo, nil
}

func (i *ImageTool) ListImages() (*kubeapi.ListImagesResponse, error) {
	var cList *C.virStorageVolPtr
	count := C.virStoragePoolListAllVolumes(i.pool, (**C.virStorageVolPtr)(&cList), 0)
	if count < 0 {
		return nil, GetLastError()
	}
	header := reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(cList)),
		Len:  int(count),
		Cap:  int(count),
	}
	volumes := *(*[]C.virStorageVolPtr)(unsafe.Pointer(&header))

	images := make([]*kubeapi.Image, 0, count)

	for _, volume := range volumes {
		id := C.GoString(C.virStorageVolGetName(volume))
		volInfo, err := i.volGetInfo(volume)
		if err != nil {
			return nil, err
		}
		size := uint64(volInfo.capacity)

		images = append(images, &kubeapi.Image{
			Id:       &id,
			RepoTags: []string{id},
			Size_:    &size,
		})
	}

	response := &kubeapi.ListImagesResponse{Images: images}
	return response, nil
}

func (i *ImageTool) ImageStatus(in *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	name := *in.Image.Image
	vol, err := i.lookupVol(name)
	if err != nil {
		return nil, err
	}
	volInfo, err := i.volGetInfo(vol)
	if err != nil {
		return nil, err
	}

	size := uint64(volInfo.capacity)
	image := &kubeapi.Image{
		Id:       &name,
		RepoTags: []string{name},
		Size_:    &size,
	}
	return &kubeapi.ImageStatusResponse{Image: image}, nil
}

func (i *ImageTool) PullImage(in *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	// TODO(nhlfr): Handle AuthConfig from PullImageRequest.
	name := *in.Image.Image
	filepath, shortName, err := download.DownloadFile(name)
	if err != nil {
		return nil, err
	}
	volXML := i.storageBackend.GenerateVolXML(i.pool, shortName, 5, "G")

	cShortName := C.CString(shortName)
	defer C.free(unsafe.Pointer(cShortName))
	cFilepath := C.CString(filepath)
	defer C.free(unsafe.Pointer(cFilepath))
	cVolXML := C.CString(volXML)
	defer C.free(unsafe.Pointer(cVolXML))

	status := C.pullImage(i.conn, i.pool, cShortName, cFilepath, cVolXML)
	if status < 0 {
		return nil, GetLastError()
	}
	if status > 0 {
		return nil, syscall.Errno(status)
	}

	return &kubeapi.PullImageResponse{}, nil
}

func (i *ImageTool) RemoveImage(in *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	name := *in.Image.Image
	vol, err := i.lookupVol(name)
	if err != nil {
		return nil, err
	}
	if status := C.virStorageVolDelete(vol, 0); status != 0 {
		return nil, GetLastError()
	}
	return &kubeapi.RemoveImageResponse{}, nil
}
