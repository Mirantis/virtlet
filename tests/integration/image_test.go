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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

type imageTester struct {
	t                  *testing.T
	manager            *VirtletManager
	imageServiceClient kubeapi.ImageServiceClient
}

func newImageTester(t *testing.T) *imageTester {
	manager := NewVirtletManager(t)
	manager.Run()
	imageServiceClient := kubeapi.NewImageServiceClient(manager.conn)
	return &imageTester{t, manager, imageServiceClient}
}

func (it *imageTester) stop() {
	it.manager.Close()
}

func (it *imageTester) pullImage(url, expectedRef string) {
	imageSpec := &kubeapi.ImageSpec{Image: url}
	in := &kubeapi.PullImageRequest{
		Image:         imageSpec,
		Auth:          &kubeapi.AuthConfig{},
		SandboxConfig: &kubeapi.PodSandboxConfig{},
	}

	if resp, err := it.imageServiceClient.PullImage(context.Background(), in); err != nil {
		it.t.Fatalf("PullImage() failed: %v", err)
	} else if resp.ImageRef != expectedRef {
		it.t.Fatalf("PullImage(): bad ImageRef in the response: %q instead of %q", resp.ImageRef, expectedRef)
	}
}

func (it *imageTester) pullSampleImage() {
	it.pullImage(imageCirrosUrl, imageCirrosRef)
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

	if image.Size_ != uint64(cirrosImageSize) {
		it.t.Fatalf("bad image size in bytes: %d instead of %d", image.Size_, cirrosImageSize)
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

func (it *imageTester) getImageFileModificationTime() (time.Time, error) {
	fi, err := os.Stat(filepath.Join(it.manager.tempDir, "images/data/"+imageCirrosSha256))
	if err != nil {
		return time.Time{}, err
	}

	return fi.ModTime(), nil
}

func TestImagePull(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullSampleImage()
}

func TestImageStatus(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()

	it.verifyNoImage()
	it.pullSampleImage()
	it.verifyImage(it.queryImage())
}

func TestRemoveImage(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullSampleImage()

	imageSpec := &kubeapi.ImageSpec{Image: imageCirrosUrl}
	in := &kubeapi.RemoveImageRequest{
		Image: imageSpec,
	}
	if _, err := it.imageServiceClient.RemoveImage(context.Background(), in); err != nil {
		t.Fatalf("RemoveImage() failed: %v", err)
	}
	it.verifyNoImage()

	// re-pull should work correctly after image removal
	it.pullSampleImage()
	it.verifyImage(it.queryImage())
}

func TestListImages(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullSampleImage()
	it.verifySingleImageListed(nil)
}

func TestListImagesWithFilter(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()
	it.pullSampleImage()
	noSuchImage := "example.com/no-such-image"
	it.verifyNoImagesListed(&kubeapi.ImageFilter{Image: &kubeapi.ImageSpec{Image: noSuchImage}})
	it.verifySingleImageListed(&kubeapi.ImageFilter{Image: &kubeapi.ImageSpec{Image: imageCirrosUrl}})
}

func TestImageRedownload(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()

	it.pullSampleImage()
	firstTime, err := it.getImageFileModificationTime()
	if err != nil {
		t.Fatal("Can't stat cirros image in libvirt store:", err)
	}

	it.pullSampleImage()

	secondTime, err := it.getImageFileModificationTime()
	if err != nil {
		t.Fatal("Can't stat cirros image in libvirt store:", err)
	}

	// the file is the same so no modifications happen
	// TODO: test downloading modified image
	if !firstTime.Equal(secondTime) {
		t.Fatal("Image in libvirt store was modified by second call to PullImage")
	}
}

func TestImagesWithSameName(t *testing.T) {
	it := newImageTester(t)
	defer it.stop()

	it.pullSampleImage()
	it.pullImage(imageCopyCirrosUrl, imageCopyCirrosRef)

	imagesCount := len(it.listImages(nil))
	if imagesCount != 2 {
		t.Fatal("Expected two images in store, get: %d", imagesCount)
	}
}
