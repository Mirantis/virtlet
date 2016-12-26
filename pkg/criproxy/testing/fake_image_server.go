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

// TODO: credits
// (based on fake_image_service.go from k8s)
package testing

import (
	"sync"

	"golang.org/x/net/context"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type FakeImageServer struct {
	sync.Mutex

	FakeImageSize uint64
	Called        []string
	Images        map[string]*runtimeapi.Image
}

func (r *FakeImageServer) SetFakeImages(images []string) {
	r.Lock()
	defer r.Unlock()

	r.Images = make(map[string]*runtimeapi.Image)
	for _, image := range images {
		r.Images[image] = r.makeFakeImage(image)
	}
}

func (r *FakeImageServer) SetFakeImageSize(size uint64) {
	r.Lock()
	defer r.Unlock()

	r.FakeImageSize = size
}

func NewFakeImageServer() *FakeImageServer {
	return &FakeImageServer{
		Called: make([]string, 0),
		Images: make(map[string]*runtimeapi.Image),
	}
}

func (r *FakeImageServer) makeFakeImage(image string) *runtimeapi.Image {
	return &runtimeapi.Image{
		Id:       &image,
		Size_:    &r.FakeImageSize,
		RepoTags: []string{image},
	}
}

func (r *FakeImageServer) ListImages(ctx context.Context, in *runtimeapi.ListImagesRequest) (*runtimeapi.ListImagesResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "ListImages")

	filter := in.GetFilter()
	images := make([]*runtimeapi.Image, 0)
	for _, img := range r.Images {
		if filter != nil && filter.Image != nil {
			imageName := filter.Image.GetImage()
			found := false
			for _, tag := range img.RepoTags {
				if imageName == tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		images = append(images, img)
	}
	return &runtimeapi.ListImagesResponse{Images: images}, nil
}

func (r *FakeImageServer) ImageStatus(ctx context.Context, in *runtimeapi.ImageStatusRequest) (*runtimeapi.ImageStatusResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "ImageStatus")

	image := in.GetImage()
	return &runtimeapi.ImageStatusResponse{Image: r.Images[image.GetImage()]}, nil
}

func (r *FakeImageServer) PullImage(ctx context.Context, in *runtimeapi.PullImageRequest) (*runtimeapi.PullImageResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "PullImage")

	// ImageID should be randomized for real container runtime, but here just use
	// image's name for easily making fake images.
	image := in.GetImage()
	imageID := image.GetImage()
	if _, ok := r.Images[imageID]; !ok {
		r.Images[imageID] = r.makeFakeImage(image.GetImage())
	}

	return &runtimeapi.PullImageResponse{}, nil
}

func (r *FakeImageServer) RemoveImage(ctx context.Context, in *runtimeapi.RemoveImageRequest) (*runtimeapi.RemoveImageResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "RemoveImage")

	// Remove the image
	image := in.GetImage()
	delete(r.Images, image.GetImage())

	return &runtimeapi.RemoveImageResponse{}, nil
}
