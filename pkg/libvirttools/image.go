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
	"net/url"
	"os"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
	libvirt "github.com/libvirt/libvirt-go"
)

const (
	defaultDownloadProtocol = "https"
)

func stripTagFromImageName(imageName string) string {
	return strings.Split(imageName, ":")[0]
}

func ImageNameToVolumeName(imageName string) (string, error) {
	u, err := url.Parse(stripTagFromImageName(imageName))
	if err != nil {
		return "", err
	}
	segments := strings.Split(u.Path, "/")
	volumeName := segments[len(segments)-1]

	return volumeName, nil
}

type ImageTool struct {
	protocol string
	tool     *StorageTool
}

func NewImageTool(conn *libvirt.Connect, poolName, protocol string) (*ImageTool, error) {
	storageTool, err := NewStorageTool(conn, poolName, "")
	if err != nil {
		return nil, err
	}
	if protocol == "" {
		protocol = defaultDownloadProtocol
	}
	return &ImageTool{tool: storageTool, protocol: protocol}, nil
}

func (i *ImageTool) ListImagesAsVolumeInfos() ([]*VolumeInfo, error) {
	return i.tool.ListVolumes()
}

func (i *ImageTool) ImageAsVolumeInfo(volumeName string) (*VolumeInfo, error) {
	vol, err := i.tool.LookupVolume(volumeName)
	if err != nil {
		return nil, err
	}
	return vol.Info()
}

func (i *ImageTool) PullRemoteImageToVolume(imageName, volumeName string) error {
	// TODO(nhlfr): Handle AuthConfig from PullImageRequest.
	path, err := utils.DownloadFile(i.protocol, stripTagFromImageName(imageName))
	if err != nil {
		return err
	}
	defer func() {
		os.Remove(path)
	}()

	return i.tool.PullFileToVolume(path, volumeName)
}

func (i *ImageTool) RemoveImage(volumeName string) error {
	return i.tool.RemoveVolume(volumeName)
}

func (i *ImageTool) GetStorageTool() *StorageTool {
	return i.tool
}
