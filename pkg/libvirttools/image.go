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
*/
import "C"

import (
	"net/url"
	"strings"

	"github.com/Mirantis/virtlet/pkg/download"
)

type ImageTool struct {
	tool *StorageTool
}

func cleanURIImageName(name string) string {
	u, err := url.Parse(name)
	if err != nil {
		return name
	}
	segments := strings.Split(u.Path, "/")
	return segments[len(segments)-1]
}

func NewImageTool(conn C.virConnectPtr, poolName string) (*ImageTool, error) {
	storageTool, err := NewStorageTool(conn, poolName)
	if err != nil {
		return nil, err
	}
	return &ImageTool{tool: storageTool}, nil
}

func (i *ImageTool) ListImagesAsVolumeInfos() ([]*VolumeInfo, error) {
	return i.tool.ListVolumes()
}

func (i *ImageTool) ImageAsVolumeInfo(name string) (*VolumeInfo, error) {
	vol, err := i.tool.LookupVolume(name)
	if err != nil {
		return nil, err
	}
	return vol.Info()
}

func (i *ImageTool) PullImageToVolume(name string) (string, error) {
	// TODO(nhlfr): Handle AuthConfig from PullImageRequest.
	filepath, shortName, err := download.DownloadFile(name)
	if err != nil {
		return "", err
	}

	return i.tool.PullImageToVolume(name, filepath, shortName)
}

func (i *ImageTool) RemoveImage(name string) error {
	return i.tool.RemoveVolume(name)
}
