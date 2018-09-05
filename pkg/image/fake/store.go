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
	"context"
	"fmt"
	"sort"

	"github.com/docker/distribution/reference"
	digest "github.com/opencontainers/go-digest"

	"github.com/Mirantis/virtlet/pkg/image"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

const (
	fakeStoreMountpoint = "/var/lib/virtlet"
	fakeUsedBytes       = 1024 * 1024 * 1024
	fakeUsedInodes      = 1024
)

// FakeStore is a fake implementation of Store interface for testing.
type FakeStore struct {
	rec       testutils.Recorder
	images    map[string]*image.Image
	refGetter image.RefGetter
}

var _ image.Store = &FakeStore{}

// NewFakeStore creates a new FakeStore.
func NewFakeStore(rec testutils.Recorder) *FakeStore {
	return &FakeStore{
		rec:    rec,
		images: make(map[string]*image.Image),
	}
}

// ListImages implements ListImages method of ImageStore interface.
func (s *FakeStore) ListImages(filter string) ([]*image.Image, error) {
	r := make([]*image.Image, 0, len(s.images))
	for _, img := range s.images {
		if filter == "" || img.Name == filter {
			r = append(r, img)
		}
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Name < r[j].Name })
	return r, nil
}

// ImageStatus implements ImageStatus method of Store interface.
func (s *FakeStore) ImageStatus(name string) (*image.Image, error) {
	name, _ = image.SplitImageName(name)
	return s.images[name], nil
}

// PullImage implements PullImage method of Store interface.
func (s *FakeStore) PullImage(ctx context.Context, name string, translator image.Translator) (string, error) {
	name, _ = image.SplitImageName(name)
	ep := translator(ctx, name)
	d := digest.FromString(name)
	named, err := reference.WithName(name)
	if err != nil {
		return "", err
	}
	withDigest, err := reference.WithDigest(named, d)
	if err != nil {
		return "", err
	}
	s.images[name] = &image.Image{
		Digest: d.String(),
		Name:   name,
		Path:   "/fake/volume/" + name,
		Size:   uint64(len(name)),
	}
	s.rec.Rec("PullImage", map[string]interface{}{
		"url":   ep.URL,
		"image": s.images[name],
	})
	return withDigest.String(), nil
}

// RemoveImage implements RemoveImage method of Store interface.
func (s *FakeStore) RemoveImage(name string) error {
	delete(s.images, name)
	s.rec.Rec("RemoveImage", name)
	return nil
}

// GC implements GC method of Store interface.
func (s *FakeStore) GC() error {
	var err error
	var refSet map[string]bool
	if s.refGetter != nil {
		refSet, err = s.refGetter()
		if err != nil {
			return err
		}
	}
	s.rec.Rec("GC", map[string]interface{}{
		"refSet": refSet,
	})
	return nil
}

// GetImagePathAndVirtualSize implements GC method of Store interface.
func (s *FakeStore) GetImagePathAndVirtualSize(imageName string) (string, uint64, error) {
	img, found := s.images[imageName]
	if !found {
		return "", 0, fmt.Errorf("image not found: %q", imageName)
	}
	return img.Path, img.Size, nil
}

// SetRefGetter implements SetRefGetter method of Store interface.
func (s *FakeStore) SetRefGetter(imageRefGetter image.RefGetter) {
	s.refGetter = imageRefGetter
}

// FilesystemStats implements FilesystemStats method from Store interface.
func (s *FakeStore) FilesystemStats() (*image.FilesystemStats, error) {
	return &image.FilesystemStats{
		Mountpoint: fakeStoreMountpoint,
		UsedBytes:  fakeUsedBytes,
		UsedInodes: fakeUsedInodes,
	}, nil
}
