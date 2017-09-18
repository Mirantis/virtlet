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

	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/imagetranslation"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type ImageTool struct {
	pool       virt.VirtStoragePool
	downloader utils.Downloader
}

type ImagePullError struct {
	message    string
	InnerError error
}

func (e ImagePullError) Error() string {
	if e.InnerError == nil {
		return e.message
	}
	return fmt.Sprintf("%s: %v", e.message, e.InnerError)
}

var _ ImageManager = &ImageTool{}

func NewImageTool(conn virt.VirtStorageConnection, downloader utils.Downloader, poolName string) (*ImageTool, error) {
	pool, err := ensureStoragePool(conn, poolName)
	if err != nil {
		return nil, err
	}
	return &ImageTool{pool: pool, downloader: downloader}, nil
}

func (i *ImageTool) ListVolumes() ([]virt.VirtStorageVolume, error) {
	return i.pool.ListAllVolumes()
}

func (i *ImageTool) ImageAsVolume(volumeName string) (virt.VirtStorageVolume, error) {
	return i.pool.LookupVolumeByName(volumeName)
}

func (i *ImageTool) fileToVolume(path, volumeName string) (virt.VirtStorageVolume, error) {
	imageSize, err := getFileSize(path)
	if err != nil {
		return nil, err
	}
	libvirtFilePath := fmt.Sprintf("/var/lib/libvirt/images/%s", volumeName)
	return i.pool.ImageToVolume(&libvirtxml.StorageVolume{
		Name:       volumeName,
		Allocation: &libvirtxml.StorageVolumeSize{Value: 0},
		Capacity:   &libvirtxml.StorageVolumeSize{Unit: "b", Value: imageSize},
		Target:     &libvirtxml.StorageVolumeTarget{Path: libvirtFilePath},
	}, path)
}

func (i *ImageTool) PullRemoteImageToVolume(imageName, volumeName string, nameTranslator imagetranslation.ImageNameTranslator) (virt.VirtStorageVolume, error) {
	imageName = stripTagFromImageName(imageName)
	endpoint := nameTranslator.Translate(imageName)
	if endpoint.Url == "" {
		endpoint = utils.Endpoint{Url: imageName}
		glog.V(1).Infof("Using URL %q without translation", imageName)
	} else {
		glog.V(1).Infof("URL %q was translated to %q", imageName, endpoint.Url)
	}

	// TODO(nhlfr): Handle AuthConfig from PullImageRequest.
	path, err := i.downloader.DownloadFile(endpoint)
	if err == nil {
		defer os.Remove(path)
		var vsv virt.VirtStorageVolume
		vsv, err = i.fileToVolume(path, volumeName)
		if err == nil {
			return vsv, nil
		}
	}
	return nil, ImagePullError{
		message:    fmt.Sprintf("error pulling image %q from %q", imageName, endpoint.Url),
		InnerError: err,
	}
}

func (i *ImageTool) RemoveImage(volumeName string) error {
	return i.pool.RemoveVolumeByName(volumeName)
}

func (i *ImageTool) GetImageVolume(imageName string) (virt.VirtStorageVolume, error) {
	imageVolumeName, err := ImageNameToVolumeName(imageName)
	if err != nil {
		return nil, err
	}

	return i.pool.LookupVolumeByName(imageVolumeName)
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

func getFileSize(path string) (uint64, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return uint64(fileInfo.Size()), nil
}
