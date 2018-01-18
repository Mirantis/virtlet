/*
Copyright 2018 Mirantis

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

package image

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

func sha256str(s string) string {
	h := sha256.New()
	if _, err := io.WriteString(h, s); err != nil {
		log.Panicf("sha256 error: %v", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

type fakeDownloader struct {
	t *testing.T
}

var _ Downloader = &fakeDownloader{}

// newFakeDownloader returns a fake downloader that writes the
// endpoint's url passed to it into the file instead of actually
// downloading it.
func newFakeDownloader(t *testing.T) *fakeDownloader {
	return &fakeDownloader{t}
}

func (d *fakeDownloader) DownloadFile(endpoint Endpoint, w io.Writer) error {
	if f, ok := w.(*os.File); ok {
		d.t.Logf("fakeDownloader: writing %q to %q", endpoint.Url, f.Name())
	}
	// add "###" prefix to endpoint URL to make the contents
	// more easily distinguishable from the URLs themselves
	// in the test code
	if n, err := w.Write([]byte("###" + endpoint.Url)); err != nil {
		return fmt.Errorf("WriteString(): %v", err)
	} else if n < len(endpoint.Url) {
		return fmt.Errorf("WriteString(): short write")
	}
	return nil
}

func fakeVirtualSize(imagePath string) (uint64, error) {
	if fi, err := os.Stat(imagePath); err != nil {
		return 0, err
	} else {
		return uint64(fi.Size()) + 1000, nil
	}
}

type ifsTester struct {
	t                *testing.T
	tmpDir           string
	store            *ImageFileStore
	images           []*Image
	refs             []string
	translatorPrefix string
}

func newIfsTester(t *testing.T) *ifsTester {
	tmpDir, err := ioutil.TempDir("", "images")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}

	tst := &ifsTester{
		t:      t,
		tmpDir: tmpDir,
		store:  NewImageFileStore(tmpDir, newFakeDownloader(t), fakeVirtualSize),
	}
	tst.images, tst.refs = tst.sampleImages()
	return tst
}

func (tst *ifsTester) teardown() {
	os.RemoveAll(tst.tmpDir)
}

func (tst *ifsTester) translateImageName(name string) Endpoint {
	if name == "foobar" {
		name = "baz"
	}
	return Endpoint{Url: tst.translatorPrefix + name, MaxRedirects: -1}
}

func (tst *ifsTester) subpath(p string) string {
	return filepath.Join(tst.tmpDir, p)
}

func (tst *ifsTester) sampleImages() ([]*Image, []string) {
	var images []*Image
	var refs []string
	for _, imageName := range []string{"example.com:1234/foo/bar", "baz"} {
		sha256 := sha256str("###" + imageName)
		image := &Image{
			// fakeDownloader writes URL to the image file,
			// and the image digest contains sha256 of the file
			Digest: "sha256:" + sha256,
			Name:   imageName,
			Path:   tst.subpath("data/" + sha256),
			Size:   uint64(len(imageName) + 3),
		}
		images = append(images, image)
		refs = append(refs, image.Name+"@"+image.Digest)
	}
	sameDataImage := *images[1]
	sameDataImage.Name = "foobar" // translated to baz by the fake translator
	return append(images, &sameDataImage), append(refs, sameDataImage.Name+"@"+sameDataImage.Digest)
}

func (tst *ifsTester) verifyFileContents(p string, expectedContents string) {
	if bs, err := ioutil.ReadFile(p); err != nil {
		tst.t.Errorf("can't verify the contents of %q: %v", p, err)
	} else if string(bs) != expectedContents {
		tst.t.Errorf("bad contents of %q: %q instead of %q", p, bs, expectedContents)
	}
}

func (tst *ifsTester) verifySubpathContents(p string, expectedContents string) {
	tst.verifyFileContents(tst.subpath(p), expectedContents)
}

func (tst *ifsTester) verifyListImages(filter string, expectedImages ...*Image) {
	switch images, err := tst.store.ListImages(filter); {
	case err != nil:
		tst.t.Errorf("ListImages(): %v", err)
	case len(expectedImages) == 0 && len(images) == 0:
		return
	case reflect.DeepEqual(images, expectedImages):
		return
	default:
		tst.t.Errorf("ListImages(): bad result:\n%s\n-- instead of --\n%s", spew.Sdump(images), spew.Sdump(expectedImages))
	}
}

func (tst *ifsTester) verifyImage(ref string, expectedContents string) {
	if path, vsize, err := tst.store.GetImagePathAndVirtualSize(ref); err != nil {
		tst.t.Errorf("GetImagePathAndVirtualSize(): %v", err)
	} else {
		tst.verifyFileContents(path, expectedContents)
		expectedVirtualSize := uint64(len(expectedContents)) + 1000
		if vsize != expectedVirtualSize {
			tst.t.Errorf("bad virtual size: %d instead of %d", vsize, expectedVirtualSize)
		}
	}
}

func (tst *ifsTester) verifyImageStatus(name string, expectedImage *Image) {
	switch image, err := tst.store.ImageStatus(name); {
	case err != nil:
		tst.t.Errorf("ImageStatus(): %v", err)
	case reflect.DeepEqual(image, expectedImage):
		return
	default:
		tst.t.Errorf("ImageStatus(): bad result:\n%s\n-- instead of --\n%s", spew.Sdump(image), spew.Sdump(expectedImage))
	}
}

func (tst *ifsTester) verifyDataDirIsEmpty() {
	items, err := filepath.Glob(filepath.Join(tst.tmpDir, "data/*"))
	if err != nil {
		tst.t.Fatalf("Glob(): %v", err)
	}
	if len(items) != 0 {
		tst.t.Errorf("unexpected files found: %v", items)
	}
}

func (tst *ifsTester) pullImage(name, ref string) {
	if s, err := tst.store.PullImage(name, tst.translateImageName); err != nil {
		tst.t.Errorf("PullImage(): %v", err)
	} else if s != ref {
		tst.t.Errorf("bad image ref returned: %q instead of %q", s, ref)
	}
}

func (tst *ifsTester) pullAllImages() {
	for n, image := range tst.images {
		tst.pullImage(image.Name, tst.refs[n])
	}
	tst.verifyListImages("", tst.images[1], tst.images[0], tst.images[2]) // alphabetically sorted by name
}

func TestImagePullListStatus(t *testing.T) {
	tst := newIfsTester(t)
	defer tst.teardown()
	tst.verifyListImages("")
	tst.verifyListImages("foobar")

	tst.pullImage(tst.images[0].Name, tst.refs[0])
	tst.verifyListImages("foobar")
	tst.verifyImageStatus("foobar", nil)
	tst.verifyListImages("", tst.images[0])
	tst.verifyListImages(tst.images[0].Name, tst.images[0])
	tst.verifySubpathContents("links/example.com:1234%foo%bar", "###example.com:1234/foo/bar")
	tst.verifyImage(tst.refs[0], "###example.com:1234/foo/bar")
	tst.verifyImage(tst.images[0].Name, "###example.com:1234/foo/bar")
	tst.verifyImage(tst.images[0].Digest, "###example.com:1234/foo/bar")
	tst.verifyImageStatus(tst.images[0].Name, tst.images[0])

	tst.pullImage(tst.images[1].Name+":latest", tst.refs[1])
	tst.verifyListImages("", tst.images[1], tst.images[0]) // alphabetically sorted by name
	tst.verifyListImages(tst.images[0].Name, tst.images[0])
	tst.verifyListImages(tst.images[1].Name, tst.images[1])
	tst.verifySubpathContents("links/example.com:1234%foo%bar", "###example.com:1234/foo/bar")
	tst.verifySubpathContents("links/baz", "###baz")
	tst.verifyImage(tst.refs[0], "###example.com:1234/foo/bar")
	tst.verifyImage(tst.refs[1], "###baz")
	tst.verifyImageStatus(tst.images[0].Name, tst.images[0])
	tst.verifyImageStatus(tst.images[1].Name, tst.images[1])

	tst.pullImage(tst.images[2].Name, tst.refs[2])
	tst.verifyListImages("", tst.images[1], tst.images[0], tst.images[2]) // alphabetically sorted by name
	tst.verifySubpathContents("links/foobar", "###baz")
}

func TestReplaceImage(t *testing.T) {
	tst := newIfsTester(t)
	defer tst.teardown()
	tst.pullAllImages()
	tst.translatorPrefix = "xx"
	sha256 := sha256str("###xxbaz")
	updatedImage := &Image{
		Digest: "sha256:" + sha256,
		Name:   tst.images[1].Name,
		Path:   tst.subpath("data/" + sha256),
		Size:   uint64(8),
	}
	updatedRef := updatedImage.Name + "@" + updatedImage.Digest
	tst.pullImage(updatedImage.Name, updatedRef)
	tst.verifyListImages("", updatedImage, tst.images[0], tst.images[2]) // alphabetically sorted by name
	tst.verifySubpathContents("links/example.com:1234%foo%bar", "###example.com:1234/foo/bar")
	tst.verifySubpathContents("links/baz", "###xxbaz")
	tst.verifySubpathContents("links/foobar", "###baz")
	tst.verifyImage(tst.refs[0], "###example.com:1234/foo/bar")
	tst.verifyImage(updatedRef, "###xxbaz")
	tst.verifyImage(tst.refs[2], "###baz")
	tst.verifyImageStatus(tst.images[0].Name, tst.images[0])
	tst.verifyImageStatus(updatedImage.Name, updatedImage)
	tst.verifyImageStatus(tst.images[2].Name, tst.images[2])
}

func TestRemoveImage(t *testing.T) {
	tst := newIfsTester(t)
	defer tst.teardown()
	tst.pullAllImages()

	for i := 0; i < 3; i++ {
		if err := tst.store.RemoveImage(tst.images[1].Name); err != nil {
			t.Errorf("RemoveImage(): %v", err)
		}
		tst.verifyListImages("", tst.images[0], tst.images[2]) // alphabetically sorted by name
		tst.verifySubpathContents("links/example.com:1234%foo%bar", "###example.com:1234/foo/bar")
		tst.verifySubpathContents("links/foobar", "###baz")
	}

	if err := tst.store.RemoveImage(tst.images[2].Name); err != nil {
		t.Errorf("RemoveImage(): %v", err)
	}
	tst.verifyListImages("", tst.images[0]) // alphabetically sorted by name
	tst.verifySubpathContents("links/example.com:1234%foo%bar", "###example.com:1234/foo/bar")

	if err := tst.store.RemoveImage(tst.images[0].Name); err != nil {
		t.Errorf("RemoveImage(): %v", err)
	}
	tst.verifyListImages("") // alphabetically sorted by name
	tst.verifyDataDirIsEmpty()
}

// TODO: image gc (rm unrefd images, rm part_* files)
//       needs to take a list of image refs that are in use
//       (name, digest or full ref)
