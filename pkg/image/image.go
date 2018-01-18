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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/distribution/digest"
	"github.com/docker/distribution/reference"
	"github.com/golang/glog"
)

type Image struct {
	Digest string
	Name   string
	Path   string
	Size   uint64
}

// ImageTranslator translates image name to a Endpoint
type ImageTranslator func(string) Endpoint

type ImageStore interface {
	ListImages(filter string) ([]*Image, error)
	ImageStatus(name string) (*Image, error)
	PullImage(name string, translator ImageTranslator) (string, error)
	RemoveImage(name string) error
	GC() error
	GetImagePathAndVirtualSize(ref string) (string, uint64, error)
}

type VirtualSizeFunc func(string) (uint64, error)

// ImageFileStore implements ImageStore
// The images are stored like this:
// /var/lib/virtlet/images
//   links/
//     example.com%whatever%etc -> ../data/2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881
//     example.com%same%image   -> ../data/2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881
//     anotherimg               -> ../data/a1fce4363854ff888cff4b8e7875d600c2682390412a8cf79b37d0b11148b0fa
//   data/
//     2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881
//     a1fce4363854ff888cff4b8e7875d600c2682390412a8cf79b37d0b11148b0fa
//
// The files are downloaded to data/
//
// Files are named part_SOME_RANDOM_STRING while being downloaded.
// After the download finishes, sha256 is calculated to be used as the
// data file name, and if the file with that name already exists, the
// newly downloaded file is removed, otherwise it's renamed to that
// sha256 digest string. In both cases a symbolic link is created
// with the name equal to docker image name but with '/' replaced by '%',
// with the link target being the matching data file.
//
// The image store performs GC upon Virtlet startup, which consists
// of removing any part_* files and those files in data/ which
// have no symlinks leading to them.
type ImageFileStore struct {
	sync.Mutex
	dir        string
	downloader Downloader
	vsizeFunc  VirtualSizeFunc
}

var _ ImageStore = &ImageFileStore{}

func NewImageFileStore(dir string, downloader Downloader, vsizeFunc VirtualSizeFunc) *ImageFileStore {
	if vsizeFunc == nil {
		vsizeFunc = GetImageVirtualSize
	}
	return &ImageFileStore{
		dir:        dir,
		downloader: downloader,
		vsizeFunc:  vsizeFunc,
	}
}

func (s *ImageFileStore) linkDir() string {
	return filepath.Join(s.dir, "links")
}

func (s *ImageFileStore) dataDir() string {
	return filepath.Join(s.dir, "data")
}

func (s *ImageFileStore) dataFileName(sha256 string) string {
	return filepath.Join(s.dataDir(), sha256)
}

func (s *ImageFileStore) linkFileName(imageName string) string {
	imageName = stripTags(imageName)
	return filepath.Join(s.linkDir(), strings.Replace(imageName, "/", "%", -1))
}

func (s *ImageFileStore) renameIfNewOrDelete(oldPath string, newPath string) (bool, error) {
	switch _, err := os.Stat(newPath); {
	case err == nil:
		if err := os.Remove(oldPath); err != nil {
			return false, fmt.Errorf("error removing %q: %v", oldPath, err)
		}
		return false, nil
	case os.IsNotExist(err):
		return true, os.Rename(oldPath, newPath)
	default:
		return false, err
	}
}

func (s *ImageFileStore) placeImage(tempPath string, dataName string, imageName string) error {
	s.Lock()
	defer s.Unlock()

	dataPath := s.dataFileName(dataName)
	isNew, err := s.renameIfNewOrDelete(tempPath, dataPath)
	if err != nil {
		return fmt.Errorf("error placing the image %q to %q: %v", imageName, dataName, err)
	}

	if err := os.MkdirAll(s.linkDir(), 0777); err != nil {
		return fmt.Errorf("mkdir %q: %v", s.linkDir(), err)
	}

	linkFileName := s.linkFileName(imageName)
	switch _, err := os.Stat(linkFileName); {
	case err == nil:
		if err := os.Remove(linkFileName); err != nil {
			return fmt.Errorf("error removing old symlink %q: %v", linkFileName, err)
		}
	case os.IsNotExist(err):
		// let's create the link
	default:
		return fmt.Errorf("error checking for symlink %q: %v", linkFileName, err)
	}

	if err := os.Symlink(filepath.Join("../data/", dataName), linkFileName); err != nil {
		if isNew {
			if err := os.Remove(dataPath); err != nil {
				glog.Warning("error removing %q: %v", dataPath, err)
			}
		}
		return fmt.Errorf("error creating symbolic link %q for image %q: %v", linkFileName, imageName, err)
	}
	return nil
}

func (s *ImageFileStore) linkDirExists() (bool, error) {
	switch _, err := os.Stat(s.linkDir()); {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("error checking for link dir %q: %v", s.linkDir(), err)
	}
}

func (s *ImageFileStore) imageInfo(fi os.FileInfo) (*Image, error) {
	fullPath := filepath.Join(s.linkDir(), fi.Name())
	if fi.Mode()&os.ModeSymlink == 0 {
		return nil, fmt.Errorf("%q is not a symbolic link", fullPath)
	}
	dest, err := os.Readlink(fullPath)
	if err != nil {
		return nil, fmt.Errorf("error reading link %q: %v", fullPath, err)
	}
	fullDataPath := filepath.Join(s.linkDir(), dest)
	destFi, err := os.Stat(fullDataPath)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %v", fullDataPath, err)
	}
	absPath, err := filepath.Abs(fullDataPath)
	if err != nil {
		return nil, fmt.Errorf("can't get abs path for %q: %v", fullDataPath, err)
	}
	if relPath, err := filepath.Rel(s.dataDir(), absPath); err != nil {
		return nil, fmt.Errorf("checking data path %q: %v", fullDataPath, err)
	} else if strings.HasPrefix(relPath, "..") {
		return nil, fmt.Errorf("not a proper data path %q", fullDataPath)
	}
	d := digest.NewDigestFromHex(string(digest.SHA256), destFi.Name())
	return &Image{
		Digest: d.String(),
		Name:   strings.Replace(fi.Name(), "%", "/", -1),
		Path:   absPath,
		Size:   uint64(destFi.Size()),
	}, nil
}

func (s *ImageFileStore) ListImages(filter string) ([]*Image, error) {
	s.Lock()
	defer s.Unlock()
	if linkDirExists, err := s.linkDirExists(); err != nil {
		return nil, err
	} else if !linkDirExists {
		return nil, nil
	}

	infos, err := ioutil.ReadDir(s.linkDir())
	if err != nil {
		return nil, fmt.Errorf("readdir %q: %v", s.linkDir(), err)
	}

	var r []*Image
	for _, fi := range infos {
		if fi.Mode().IsDir() {
			continue
		}
		image, err := s.imageInfo(fi)
		if err != nil {
			glog.Warningf("listing images: skipping image link %q: %v", fi.Name(), err)
			continue
		}
		if filter == "" || image.Name == filter {
			r = append(r, image)
		}
	}

	return r, nil
}

func (s *ImageFileStore) imageStatusUnlocked(name string) (*Image, error) {
	linkFileName := s.linkFileName(name)
	// get info about the link itself, not its target
	switch fi, err := os.Lstat(linkFileName); {
	case err == nil:
		return s.imageInfo(fi)
	case os.IsNotExist(err):
		return nil, nil
	default:
		return nil, fmt.Errorf("can't stat %q: %v", linkFileName, err)
	}
}

func (s *ImageFileStore) ImageStatus(name string) (*Image, error) {
	s.Lock()
	defer s.Unlock()
	return s.imageStatusUnlocked(name)
}

func (s *ImageFileStore) PullImage(name string, translator ImageTranslator) (string, error) {
	name = stripTags(name)
	ep := translator(name)
	glog.V(1).Infof("Image translation: %q -> %q", name, ep.Url)
	if err := os.MkdirAll(s.dataDir(), 0777); err != nil {
		return "", fmt.Errorf("mkdir %q: %v", s.dataDir(), err)
	}
	tempFile, err := ioutil.TempFile(s.dataDir(), "part_")
	if err != nil {
		return "", fmt.Errorf("failed to create a temporary file: %v", err)
	}
	if err := s.downloader.DownloadFile(ep, tempFile); err != nil {
		tempFile.Close()
		if err := os.Remove(tempFile.Name()); err != nil {
			glog.Warningf("Error removing %q: %v", tempFile.Name(), err)
		}
		return "", fmt.Errorf("error downloading %q: %v", ep.Url, err)
	}

	if _, err := tempFile.Seek(0, os.SEEK_SET); err != nil {
		return "", fmt.Errorf("can't get the digest for %q: Seek(): %v", tempFile.Name(), err)
	}

	d, err := digest.FromReader(tempFile)
	if err != nil {
		return "", err
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("closing %q: %v", tempFile.Name(), err)
	}
	if err := s.placeImage(tempFile.Name(), d.Hex(), name); err != nil {
		return "", err
	}
	named, err := reference.WithName(name)
	if err != nil {
		return "", err
	}
	withDigest, err := reference.WithDigest(named, d)
	if err != nil {
		return "", err
	}
	return withDigest.String(), nil
}

func (s *ImageFileStore) removeIfUnreferenced(sha256 string) error {
	infos, err := ioutil.ReadDir(s.linkDir())
	if err != nil {
		return fmt.Errorf("readdir %q: %v", s.linkDir(), err)
	}
	for _, fi := range infos {
		linkFileName := filepath.Join(s.linkDir(), fi.Name())
		switch dest, err := os.Readlink(linkFileName); {
		case err != nil:
			glog.Warningf("error reading link %q: %v", linkFileName, err)
		case filepath.Base(dest) == sha256:
			// found a reference
			return nil
		}
	}
	dataFileName := s.dataFileName(sha256)
	return os.Remove(dataFileName)
}

func (s *ImageFileStore) RemoveImage(name string) error {
	s.Lock()
	defer s.Unlock()
	linkFileName := s.linkFileName(name)
	switch _, err := os.Lstat(linkFileName); {
	case err == nil:
		dest, err := os.Readlink(linkFileName)
		if err != nil {
			return fmt.Errorf("error reading link %q: %v", linkFileName, err)
		}
		if err := os.Remove(linkFileName); err != nil {
			return fmt.Errorf("can't remove %q: %v", linkFileName)
		}
		return s.removeIfUnreferenced(filepath.Base(dest))
	case os.IsNotExist(err):
		return nil
	default:
		return fmt.Errorf("can't stat %q: %v", linkFileName, err)
	}
}

func (s *ImageFileStore) GC() error {
	glog.Warning("Image GC not implemented yet")
	return nil
}

func (s *ImageFileStore) GetImagePathAndVirtualSize(ref string) (string, uint64, error) {
	s.Lock()
	defer s.Unlock()
	parsed, err := reference.Parse(ref)
	if err != nil {
		return "", 0, fmt.Errorf("bad image reference %q: %v", ref, err)
	}

	pathViaDigest := ""
	if digested, ok := parsed.(reference.Digested); ok {
		if digested.Digest().Algorithm() != digest.SHA256 {
			return "", 0, fmt.Errorf("bad image digest (need sha256): %q", digested.Digest())
		}
		pathViaDigest = s.dataFileName(digested.Digest().Hex())
	}

	pathViaName := ""
	if named, ok := parsed.(reference.Named); ok {
		linkFileName := s.linkFileName(named.Name())
		if pathViaName, err = os.Readlink(linkFileName); err != nil {
			glog.Warningf("error reading link %q: %v", pathViaName, err)
		}
		pathViaName = filepath.Join(s.linkDir(), pathViaName)
	}

	path := pathViaDigest
	switch {
	case pathViaDigest == "" && pathViaName == "":
		return "", 0, fmt.Errorf("bad image reference %q: %v", ref, err)
	case pathViaDigest == "":
		path = pathViaName
	case pathViaName != "":
		fi1, err := os.Stat(pathViaName)
		if err != nil {
			return "", 0, err
		}
		fi2, err := os.Stat(pathViaDigest)
		if err != nil {
			return "", 0, err
		}
		if !os.SameFile(fi1, fi2) {
			return "", 0, fmt.Errorf("digest / name path mismatch: %q vs %q", pathViaDigest, pathViaName)
		}
	}

	vsize, err := s.vsizeFunc(path)
	if err != nil {
		return "", 0, fmt.Errorf("error getting image size for %q: %v", path, err)
	}
	return path, vsize, nil
}

func stripTags(imageName string) string {
	ref, err := reference.Parse(imageName)
	if err != nil {
		glog.Warningf("stripTags: failed to parse image name as ref: %q: %v", imageName, err)
		return imageName
	}
	if namedTagged, ok := ref.(reference.NamedTagged); ok {
		return namedTagged.Name()
	}
	return imageName
}
