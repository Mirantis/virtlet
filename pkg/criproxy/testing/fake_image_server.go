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
	"sort"
	"sync"

	"golang.org/x/net/context"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type FakeImageServer struct {
	sync.Mutex

	journal       Journal
	FakeImageSize uint64
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

func NewFakeImageServer(journal Journal) *FakeImageServer {
	return &FakeImageServer{
		journal: journal,
		Images:  make(map[string]*runtimeapi.Image),
	}
}

func (r *FakeImageServer) makeFakeImage(image string) *runtimeapi.Image {
	return &runtimeapi.Image{
		Id:       image,
		Size_:    r.FakeImageSize,
		RepoTags: []string{image},
	}
}

func (r *FakeImageServer) ListImages(ctx context.Context, in *runtimeapi.ListImagesRequest) (*runtimeapi.ListImagesResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.journal.Record("ListImages")

	var imageNames []string
	for imageName, _ := range r.Images {
		imageNames = append(imageNames, imageName)
	}
	sort.Strings(imageNames)

	filter := in.GetFilter()
	images := make([]*runtimeapi.Image, 0)
	for _, name := range imageNames {
		// make a copy of the image struct
		img := *r.Images[name]
		if filter != nil && filter.Image != nil {
			imageName := filter.Image.Image
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

		images = append(images, &img)
	}
	return &runtimeapi.ListImagesResponse{Images: images}, nil
}

func (r *FakeImageServer) ImageStatus(ctx context.Context, in *runtimeapi.ImageStatusRequest) (*runtimeapi.ImageStatusResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.journal.Record("ImageStatus")

	image := in.GetImage()
	// make a copy of the image struct
	img := *r.Images[image.Image]
	return &runtimeapi.ImageStatusResponse{Image: &img}, nil
}

func (r *FakeImageServer) PullImage(ctx context.Context, in *runtimeapi.PullImageRequest) (*runtimeapi.PullImageResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.journal.Record("PullImage")

	// ImageID should be randomized for real container runtime, but here just use
	// image's name for easily making fake images.
	image := in.GetImage()
	imageID := image.Image
	if _, ok := r.Images[imageID]; !ok {
		r.Images[imageID] = r.makeFakeImage(image.Image)
	}

	return &runtimeapi.PullImageResponse{}, nil
}

func (r *FakeImageServer) RemoveImage(ctx context.Context, in *runtimeapi.RemoveImageRequest) (*runtimeapi.RemoveImageResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.journal.Record("RemoveImage")

	// Remove the image
	image := in.GetImage()
	delete(r.Images, image.Image)

	return &runtimeapi.RemoveImageResponse{}, nil
}
