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
	"net/url"
	"reflect"
	"strings"
	"syscall"
	"unsafe"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/download"
)

type ImageTool struct {
	conn           C.virConnectPtr
	pool           C.virStoragePoolPtr
	storageBackend StorageBackend
}

func cleanURIImageName(name string) string {
	u, err := url.Parse(name)
	if err != nil {
		return name
	}
	segments := strings.Split(u.Path, "/")
	return segments[len(segments)-1]
}

func NewImageTool(conn C.virConnectPtr, poolName string, storageBackendName string) (*ImageTool, error) {
	pool, err := LookupStoragePool(conn, poolName)
	if err != nil {
		return nil, err
	}
	storageBackend, err := GetStorageBackend(storageBackendName)
	if err != nil {
		return nil, err
	}
	return &ImageTool{
		conn:           conn,
		pool:           pool,
		storageBackend: storageBackend,
	}, nil
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
		volInfo, err := VolGetInfo(volume)
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

func (i *ImageTool) ImageStatus(name string) (*kubeapi.Image, error) {
	vol, err := LookupVol(name, i.pool)
	if err != nil {
		return nil, err
	}
	volInfo, err := VolGetInfo(vol)
	if err != nil {
		return nil, err
	}

	size := uint64(volInfo.capacity)
	image := &kubeapi.Image{
		Id:       &name,
		RepoTags: []string{name},
		Size_:    &size,
	}
	return image, nil
}

func (i *ImageTool) PullImage(name string) (string, error) {
	// TODO(nhlfr): Handle AuthConfig from PullImageRequest.
	filepath, shortName, err := download.DownloadFile(name)
	if err != nil {
		return "", err
	}
	libvirtFilepath := fmt.Sprintf("/var/lib/libvirt/images/%s", shortName)
	volXML := i.storageBackend.GenerateVolXML(i.pool, shortName, 5, "G", libvirtFilepath)

	cShortName := C.CString(shortName)
	defer C.free(unsafe.Pointer(cShortName))
	cFilepath := C.CString(filepath)
	defer C.free(unsafe.Pointer(cFilepath))
	cVolXML := C.CString(volXML)
	defer C.free(unsafe.Pointer(cVolXML))

	status := C.pullImage(i.conn, i.pool, cShortName, cFilepath, cVolXML)
	if status < 0 {
		return "", GetLastError()
	}
	if status > 0 {
		return "", syscall.Errno(status)
	}

	return libvirtFilepath, nil
}

func (i *ImageTool) RemoveImage(name string) error {
	return RemoveVol(name, i.pool)
}
