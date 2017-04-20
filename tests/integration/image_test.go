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

package integration

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type imageTester struct {
	t                  *testing.T
	manager            *VirtletManager
	imageServiceClient kubeapi.ImageServiceClient
}

func newImageTester(t *testing.T) *imageTester {
	manager := NewVirtletManager()
	if err := manager.Run(); err != nil {
		t.Fatal(err)
	}
	imageServiceClient := kubeapi.NewImageServiceClient(manager.conn)
	return &imageTester{t, manager, imageServiceClient}
}

func (it *imageTester) stop() {
	it.manager.Close()
}

func (it *imageTester) pullImage() {
	imageSpec := &kubeapi.ImageSpec{Image: imageCirrosUrl}
	in := &kubeapi.PullImageRequest{
		Image:         imageSpec,
		Auth:          &kubeapi.AuthConfig{},
		SandboxConfig: &kubeapi.PodSandboxConfig{},
	}

	if resp, err := it.imageServiceClient.PullImage(context.Background(), in); err != nil {
		it.t.Fatalf("PullImage() failed: %v", err)
	} else if resp.ImageRef != imageSpec.Image {
		it.t.Fatalf("PullImage(): bad ImageRef in the response: %q instead of %q", resp.ImageRef, imageSpec)
	}
}

func (it *imageTester) queryImage() *kubeapi.Image {
	imageSpec := &kubeapi.ImageSpec{Image: imageCirrosUrl}
	in := &kubeapi.ImageStatusRequest{
		Image: imageSpec,
	}
	resp, err := it.imageServiceClient.ImageStatus(context.Background(), in)
	if err != nil {
		it.t.Fatalf("ImageStatus() failed: %v", err)
	}
	return resp.Image
}

func (it *imageTester) listImages(filter *kubeapi.ImageFilter) []*kubeapi.Image {
	resp, err := it.imageServiceClient.ListImages(context.Background(), &kubeapi.ListImagesRequest{Filter: filter})
	if err != nil {
		it.t.Fatalf("ListImages() failed: %v", err)
	}
	return resp.Images
}

func (it *imageTester) verifyImage(image *kubeapi.Image) {
	if image == nil {
		it.t.Fatal("no image returned by ImageStatus()")
	}

	if image.Id != imageCirrosId {
		it.t.Fatalf("bad image id: %q instead of %q", image.Id, imageCirrosId)
	}

	repoTags := image.RepoTags
	if len(repoTags) != 1 {
		it.t.Fatalf("bad number of repo tags for the image (expected just 1 tag): %v", image.RepoTags)
	}
	if repoTags[0] != imageCirrosUrl {
		it.t.Fatalf("bad image repo tag: %q instead of %q", repoTags[0], imageCirrosUrl)
	}

	if image.Size_ != uint64(cirrosVolumeSize) {
		it.t.Fatalf("bad image size in bytes: %d instead of %d", image.Size_, cirrosVolumeSize)
	}

	if image.Uid != nil {
		it.t.Fatalf("bad image UID: %v instead of nil", image.GetUid())
	}
}

func (it *imageTester) verifyNoImage() {
	if it.queryImage() != nil {
		it.t.Fatal("ImageStatus() returned an image when it shouldn't")
	}
}

func (it *imageTester) verifySingleImageListed(filter *kubeapi.ImageFilter) {
	images := it.listImages(filter)
	if len(images) != 1 {
		it.t.Fatalf("Single image expected from ListImages(), but got %s", spew.Sdump(images))
	}

	it.verifyImage(images[0])
}

func (it *imageTester) verifyNoImagesListed(filter *kubeapi.ImageFilter) {
	images := it.listImages(filter)
	if len(images) != 0 {
		it.t.Fatalf("No images expected from ListImages(), but got %s", spew.Sdump(images))
	}
}

func TestImagePull(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullImage()
	// make sure existing image is handled correctly
	it.pullImage()
}

func TestImageStatus(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()

	it.verifyNoImage()
	it.pullImage()
	it.verifyImage(it.queryImage())
}

func TestRemoveImage(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullImage()

	imageSpec := &kubeapi.ImageSpec{Image: imageCirrosUrl}
	in := &kubeapi.RemoveImageRequest{
		Image: imageSpec,
	}
	if _, err := it.imageServiceClient.RemoveImage(context.Background(), in); err != nil {
		t.Fatalf("RemoveImage() failed: %v", err)
	}
	it.verifyNoImage()

	// re-pull should work correctly after image removal
	it.pullImage()
	it.verifyImage(it.queryImage())
}

func TestListImages(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullImage()
	it.verifySingleImageListed(nil)
}

func TestListImagesWithFilter(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullImage()
	noSuchImage := "example.com/no-such-image"
	it.verifyNoImagesListed(&kubeapi.ImageFilter{Image: &kubeapi.ImageSpec{Image: noSuchImage}})
	it.verifySingleImageListed(&kubeapi.ImageFilter{Image: &kubeapi.ImageSpec{Image: imageCirrosUrl}})
}
