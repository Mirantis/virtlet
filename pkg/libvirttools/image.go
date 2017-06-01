/*
Copyright 2017 Mirantis

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
	"crypto/sha1"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type ImageManager interface {
	GetImageVolume(imageName string) (virt.VirtStorageVolume, error)
}

type ImageTool struct {
	tool       *StorageTool
	downloader utils.Downloader
}

var _ ImageManager = &ImageTool{}

func NewImageTool(conn virt.VirtStorageConnection, downloader utils.Downloader, poolName string) (*ImageTool, error) {
	storageTool, err := NewStorageTool(conn, poolName, "")
	if err != nil {
		return nil, err
	}
	return &ImageTool{tool: storageTool, downloader: downloader}, nil
}

func (i *ImageTool) ListVolumes() ([]virt.VirtStorageVolume, error) {
	return i.tool.ListVolumes()
}

func (i *ImageTool) ImageAsVolume(volumeName string) (virt.VirtStorageVolume, error) {
	return i.tool.LookupVolume(volumeName)
}

func (i *ImageTool) PullRemoteImageToVolume(imageName, volumeName string) (virt.VirtStorageVolume, error) {
	// TODO(nhlfr): Handle AuthConfig from PullImageRequest.
	path, err := i.downloader.DownloadFile(stripTagFromImageName(imageName))
	if err != nil {
		return nil, err
	}
	defer func() {
		os.Remove(path)
	}()

	return i.tool.FileToVolume(path, volumeName)
}

func (i *ImageTool) RemoveImage(volumeName string) error {
	return i.tool.RemoveVolume(volumeName)
}

func (i *ImageTool) GetImageVolume(imageName string) (virt.VirtStorageVolume, error) {
	imageVolumeName, err := ImageNameToVolumeName(imageName)
	if err != nil {
		return nil, err
	}

	return i.tool.LookupVolume(imageVolumeName)
}

func ImageNameFromVirtVolumeName(volumeName string) string {
	parts := strings.SplitN(volumeName, "_", 2)
	return parts[1]
}

func stripTagFromImageName(imageName string) string {
	return strings.Split(imageName, ":")[0]
}

func ImageNameToVolumeName(imageName string) (string, error) {
	u, err := url.Parse(stripTagFromImageName(imageName))
	if err != nil {
		return "", err
	}

	h := sha1.New()
	io.WriteString(h, u.String())

	segments := strings.Split(u.Path, "/")

	volumeName := fmt.Sprintf("%x_%s", h.Sum(nil), segments[len(segments)-1])

	return volumeName, nil
}
