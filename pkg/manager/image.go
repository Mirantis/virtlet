/*
Copyright 2016-2018 Mirantis

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

package manager

import (
	"time"

	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"

	"github.com/Mirantis/virtlet/pkg/image"
)

// VirtletImageService handles CRI image service calls.
type VirtletImageService struct {
	imageStore      image.Store
	imageTranslator image.Translator
}

// NewVirtletImageService returns a new instance of VirtletImageService.
func NewVirtletImageService(imageStore image.Store, imageTranslator image.Translator) *VirtletImageService {
	return &VirtletImageService{
		imageStore:      imageStore,
		imageTranslator: imageTranslator,
	}
}

// ListImages method implements ListImages from CRI.
func (v *VirtletImageService) ListImages(ctx context.Context, in *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	images, err := v.imageStore.ListImages(in.GetFilter().GetImage().GetImage())
	if err != nil {
		return nil, err
	}

	response := &kubeapi.ListImagesResponse{Images: make([]*kubeapi.Image, len(images))}
	for n, image := range images {
		response.Images[n] = imageToKubeapi(image)
	}

	return response, err
}

// ImageStatus method implements ImageStatus from CRI.
func (v *VirtletImageService) ImageStatus(ctx context.Context, in *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	img, err := v.imageStore.ImageStatus(in.GetImage().GetImage())
	if err != nil {
		return nil, err
	}
	response := &kubeapi.ImageStatusResponse{Image: imageToKubeapi(img)}
	return response, err
}

// PullImage method implements PullImage from CRI.
func (v *VirtletImageService) PullImage(ctx context.Context, in *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	imageName := in.GetImage().GetImage()

	ref, err := v.imageStore.PullImage(ctx, imageName, v.imageTranslator)
	if err != nil {
		return nil, err
	}

	response := &kubeapi.PullImageResponse{ImageRef: ref}
	return response, nil
}

// RemoveImage method implements RemoveImage from CRI.
func (v *VirtletImageService) RemoveImage(ctx context.Context, in *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	imageName := in.GetImage().GetImage()
	if err := v.imageStore.RemoveImage(imageName); err != nil {
		return nil, err
	}
	return &kubeapi.RemoveImageResponse{}, nil
}

// ImageFsInfo returns an info about filesystem used by images service
func (v *VirtletImageService) ImageFsInfo(ctx context.Context, in *kubeapi.ImageFsInfoRequest) (*kubeapi.ImageFsInfoResponse, error) {
	stats, err := v.imageStore.FilesystemStats()
	if err != nil {
		return nil, err
	}
	return &kubeapi.ImageFsInfoResponse{
		ImageFilesystems: []*kubeapi.FilesystemUsage{
			&kubeapi.FilesystemUsage{
				Timestamp: time.Now().UnixNano(),
				FsId: &kubeapi.FilesystemIdentifier{
					Mountpoint: stats.Mountpoint,
				},
				UsedBytes:  &kubeapi.UInt64Value{Value: stats.UsedBytes},
				InodesUsed: &kubeapi.UInt64Value{Value: stats.UsedInodes},
			},
		},
	}, nil
}

func imageToKubeapi(img *image.Image) *kubeapi.Image {
	if img == nil {
		return nil
	}
	return &kubeapi.Image{
		Id:       img.Digest,
		RepoTags: []string{img.Name},
		Size_:    img.Size,
	}
}
