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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aykevl/osfs"
	"github.com/docker/distribution/reference"
	"github.com/golang/glog"
	digest "github.com/opencontainers/go-digest"

	"github.com/Mirantis/virtlet/pkg/utils"
)

// Image describes an image.
type Image struct {
	Digest string
	Name   string
	Path   string
	Size   uint64
}

func (img *Image) hexDigest() (string, error) {
	var d digest.Digest
	var err error
	if d, err = digest.Parse(img.Digest); err != nil {
		return "", err
	}
	return d.Hex(), nil
}

// Translator translates image name to a Endpoint.
type Translator func(context.Context, string) Endpoint

// RefGetter is a function that returns the list of images
// that are currently in use.
type RefGetter func() (map[string]bool, error)

// FilesystemStats contains info about filesystem mountpoint and
// space/inodes used by images on it
type FilesystemStats struct {
	// Mountpoint denotes the filesystem mount point
	Mountpoint string
	// UsedBytes is the number of bytes used by images
	UsedBytes uint64
	// UsedInodes is the number of inodes used by images
	UsedInodes uint64
}

// Store is an interface for the image store.
type Store interface {
	// ListImage returns the list of images in the store.
	// If filter is specified, the list will only contain the
	// image with the same name as the value of 'filter',
	// or no images at all if there are no such images.
	ListImages(filter string) ([]*Image, error)

	// ImageStatus returns the description of the specified image.
	// If the image doesn't exist, no error is returned, just
	// nil instead of an image.
	ImageStatus(name string) (*Image, error)

	// PullImage pulls the image using specified image name translation
	// function.
	PullImage(ctx context.Context, name string, translator Translator) (string, error)

	// RemoveImage removes the specified image.
	RemoveImage(name string) error

	// GC removes all unused or partially downloaded images.
	GC() error

	// GetImagePathAndVirtualSize returns the path to image data
	// and virtual size for the specified image. It accepts
	// an image reference or a digest.
	GetImagePathAndVirtualSize(ref string) (string, uint64, error)

	// SetRefGetter sets a function that will be used to determine
	// the set of images that are currently in use.
	SetRefGetter(imageRefGetter RefGetter)

	// FilesystemStats returns disk space and inode usage info for this store.
	FilesystemStats() (*FilesystemStats, error)
}

// VirtualSizeFunc specifies a function that returns the virtual
// size of the specified QCOW2 image file.
type VirtualSizeFunc func(string) (uint64, error)

// FileStore implements Store. For more info on its
// workings, see docs/images.md
type FileStore struct {
	sync.Mutex
	dir        string
	downloader Downloader
	vsizeFunc  VirtualSizeFunc
	refGetter  RefGetter
}

var _ Store = &FileStore{}

// NewFileStore creates a new FileStore that will be using
// the specified dir to store the images, image downloader and
// a function for getting virtual size of the image. If vsizeFunc
// is nil, the default GetImageVirtualSize function will be used.
func NewFileStore(dir string, downloader Downloader, vsizeFunc VirtualSizeFunc) *FileStore {
	if vsizeFunc == nil {
		vsizeFunc = GetImageVirtualSize
	}
	return &FileStore{
		dir:        dir,
		downloader: downloader,
		vsizeFunc:  vsizeFunc,
	}
}

func (s *FileStore) linkDir() string {
	return filepath.Join(s.dir, "links")
}

func (s *FileStore) linkDirExists() (bool, error) {
	switch _, err := os.Stat(s.linkDir()); {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("error checking for link dir %q: %v", s.linkDir(), err)
	}
}

func (s *FileStore) dataDir() string {
	return filepath.Join(s.dir, "data")
}

func (s *FileStore) dataFileName(hexDigest string) string {
	return filepath.Join(s.dataDir(), hexDigest)
}

func (s *FileStore) linkFileName(imageName string) string {
	imageName, _ = SplitImageName(imageName)
	return filepath.Join(s.linkDir(), strings.Replace(imageName, "/", "%", -1))
}

func (s *FileStore) renameIfNewOrDelete(oldPath string, newPath string) (bool, error) {
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

func (s *FileStore) getImageHexDigestsInUse() (map[string]bool, error) {
	imagesInUse := make(map[string]bool)
	var imgList []string
	if s.refGetter != nil {
		refSet, err := s.refGetter()
		if err != nil {
			return nil, fmt.Errorf("error listing images in use: %v", err)
		}
		for spec, present := range refSet {
			if present {
				imgList = append(imgList, spec)
			}
		}
	}
	for _, imgSpec := range imgList {
		if d := GetHexDigest(imgSpec); d != "" {
			imagesInUse[d] = true
		}
	}
	images, err := s.listImagesUnlocked("")
	if err != nil {
		return nil, err
	}
	for _, img := range images {
		if hexDigest, err := img.hexDigest(); err != nil {
			glog.Warningf("GC: error calculating digest for image %q: %v", img.Name, err)
		} else {
			imagesInUse[hexDigest] = true
		}
	}
	return imagesInUse, nil
}

func (s *FileStore) removeIfUnreferenced(hexDigest string) error {
	imagesInUse, err := s.getImageHexDigestsInUse()
	switch {
	case err != nil:
		return err
	case imagesInUse[hexDigest]:
		return nil
	default:
		dataFileName := s.dataFileName(hexDigest)
		return os.Remove(dataFileName)
	}
}

// removeImageUnlocked removes the specified image unless its dataFile name
// is equal to one passed us keepData. Returns true if the file did not
// exist or was removed.
func (s *FileStore) removeImageIfItsNotNeeded(name, keepData string) (bool, error) {
	linkFileName := s.linkFileName(name)
	switch _, err := os.Lstat(linkFileName); {
	case err == nil:
		dest, err := os.Readlink(linkFileName)
		if err != nil {
			return false, fmt.Errorf("error reading link %q: %v", linkFileName, err)
		}
		destName := filepath.Base(dest)
		if destName == keepData {
			return false, nil
		}
		if err := os.Remove(linkFileName); err != nil {
			return false, fmt.Errorf("can't remove %q: %v", linkFileName, err)
		}
		return true, s.removeIfUnreferenced(destName)
	case os.IsNotExist(err):
		return true, nil
	default:
		return false, fmt.Errorf("can't stat %q: %v", linkFileName, err)
	}
}

func (s *FileStore) placeImage(tempPath string, dataName string, imageName string) error {
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
		if removed, err := s.removeImageIfItsNotNeeded(imageName, dataName); err != nil {
			return fmt.Errorf("error removing old symlink %q: %v", linkFileName, err)
		} else if !removed {
			// same image with the same name
			return nil
		}
	case os.IsNotExist(err):
		// let's create the link
	default:
		return fmt.Errorf("error checking for symlink %q: %v", linkFileName, err)
	}

	if err := os.Symlink(filepath.Join("../data/", dataName), linkFileName); err != nil {
		if isNew {
			if err := os.Remove(dataPath); err != nil {
				glog.Warningf("error removing %q: %v", dataPath, err)
			}
		}
		return fmt.Errorf("error creating symbolic link %q for image %q: %v", linkFileName, imageName, err)
	}
	return nil
}

func (s *FileStore) imageInfo(fi os.FileInfo) (*Image, error) {
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

func (s *FileStore) listImagesUnlocked(filter string) ([]*Image, error) {
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

// ListImages implements ListImages method of ImageStore interface.
func (s *FileStore) ListImages(filter string) ([]*Image, error) {
	s.Lock()
	defer s.Unlock()
	return s.listImagesUnlocked(filter)
}

func (s *FileStore) imageStatusUnlocked(name string) (*Image, error) {
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

// ImageStatus implements ImageStatus method of Store interface.
func (s *FileStore) ImageStatus(name string) (*Image, error) {
	s.Lock()
	defer s.Unlock()
	return s.imageStatusUnlocked(name)
}

// PullImage implements PullImage method of Store interface.
func (s *FileStore) PullImage(ctx context.Context, name string, translator Translator) (string, error) {
	name, specDigest := SplitImageName(name)
	ep := translator(ctx, name)
	glog.V(1).Infof("Image translation: %q -> %q", name, ep.URL)
	if err := os.MkdirAll(s.dataDir(), 0777); err != nil {
		return "", fmt.Errorf("mkdir %q: %v", s.dataDir(), err)
	}
	tempFile, err := ioutil.TempFile(s.dataDir(), "part_")
	if err != nil {
		return "", fmt.Errorf("failed to create a temporary file: %v", err)
	}
	defer func() {
		if tempFile != nil {
			tempFile.Close()
		}
	}()
	if err := s.downloader.DownloadFile(ctx, ep, tempFile); err != nil {
		tempFile.Close()
		if err := os.Remove(tempFile.Name()); err != nil {
			glog.Warningf("Error removing %q: %v", tempFile.Name(), err)
		}
		return "", fmt.Errorf("error downloading %q: %v", ep.URL, err)
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
	fileName := tempFile.Name()
	tempFile = nil
	if specDigest != "" && d != specDigest {
		return "", fmt.Errorf("image digest mismatch: %s instead of %s", d, specDigest)
	}
	if err := s.placeImage(fileName, d.Hex(), name); err != nil {
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

// RemoveImage implements RemoveImage method of Store interface.
func (s *FileStore) RemoveImage(name string) error {
	s.Lock()
	defer s.Unlock()
	_, err := s.removeImageIfItsNotNeeded(name, "")
	return err
}

// GC implements GC method of Store interface.
func (s *FileStore) GC() error {
	s.Lock()
	defer s.Unlock()
	imagesInUse, err := s.getImageHexDigestsInUse()
	if err != nil {
		return err
	}
	globExpr := filepath.Join(s.dataDir(), "*")
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return fmt.Errorf("Glob(): %q: %v", globExpr, err)
	}
	for _, m := range matches {
		if imagesInUse[filepath.Base(m)] {
			continue
		}
		glog.V(1).Infof("GC: removing unreferenced image file %q", m)
		if err := os.Remove(m); err != nil {
			glog.Warningf("GC: removing %q: %v", m, err)
		}
	}
	return nil
}

// GetImagePathAndVirtualSize implements GC method of Store interface.
func (s *FileStore) GetImagePathAndVirtualSize(ref string) (string, uint64, error) {
	s.Lock()
	defer s.Unlock()
	glog.V(3).Infof("GetImagePathAndVirtualSize(): %q", ref)

	var pathViaDigest, pathViaName string
	// parsing digest as ref gives bad results
	if d, err := digest.Parse(ref); err == nil {
		if d.Algorithm() != digest.SHA256 {
			return "", 0, fmt.Errorf("bad image digest (need sha256): %q", d)
		}
		pathViaDigest = s.dataFileName(d.Hex())
	} else {
		parsed, err := reference.Parse(ref)
		if err != nil {
			return "", 0, fmt.Errorf("bad image reference %q: %v", ref, err)
		}

		if digested, ok := parsed.(reference.Digested); ok {
			if digested.Digest().Algorithm() != digest.SHA256 {
				return "", 0, fmt.Errorf("bad image digest (need sha256): %q", digested.Digest())
			}
			pathViaDigest = s.dataFileName(digested.Digest().Hex())
		}

		if named, ok := parsed.(reference.Named); ok && named.Name() != "" {
			linkFileName := s.linkFileName(named.Name())
			if pathViaName, err = os.Readlink(linkFileName); err != nil {
				glog.Warningf("error reading link %q: %v", pathViaName, err)
			} else {
				pathViaName = filepath.Join(s.linkDir(), pathViaName)
			}
		}
	}

	path := pathViaDigest
	switch {
	case pathViaDigest == "" && pathViaName == "":
		return "", 0, fmt.Errorf("bad image reference %q", ref)
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

// SetRefGetter implements SetRefGetter method of Store interface.
func (s *FileStore) SetRefGetter(imageRefGetter RefGetter) {
	s.refGetter = imageRefGetter
}

// SplitImageName parses image nmae and returns the name sans tag and
// the digest, if any.
func SplitImageName(imageName string) (string, digest.Digest) {
	ref, err := reference.Parse(imageName)
	if err != nil {
		glog.Warningf("StripTags: failed to parse image name as ref: %q: %v", imageName, err)
		return imageName, ""
	}

	named, ok := ref.(reference.Named)
	if !ok {
		return imageName, ""
	}

	if digested, ok := ref.(reference.Digested); ok {
		return named.Name(), digested.Digest()
	}

	return named.Name(), ""
}

// GetHexDigest returns the hex digest contained in imageSpec, if any,
// or an empty string if imageSpec doesn't have the spec.
func GetHexDigest(imageSpec string) string {
	if d, err := digest.Parse(imageSpec); err == nil {
		if d.Algorithm() != digest.SHA256 {
			return ""
		}
		return d.Hex()
	}

	parsed, err := reference.Parse(imageSpec)
	if err != nil {
		return ""
	}

	if digested, ok := parsed.(reference.Digested); ok && digested.Digest().Algorithm() == digest.SHA256 {
		return digested.Digest().Hex()
	}

	return ""
}

// FilesystemStats returns disk space and inode usage info for this store.
// TODO: instead of returning data from filesystem we should retrieve from
// metadata store sizes of images and sum them, or even retrieve precalculated
// sum. That's because same filesystem could be used by other things than images.
func (s *FileStore) FilesystemStats() (*FilesystemStats, error) {
	occupiedBytes, occupiedInodes, err := utils.GetFsStatsForPath(s.dir)
	if err != nil {
		return nil, err
	}
	info, err := osfs.Read()
	if err != nil {
		return nil, err
	}
	mount, err := info.GetPath(s.dir)
	if err != nil {
		return nil, err
	}
	return &FilesystemStats{
		Mountpoint: mount.FSRoot,
		UsedBytes:  occupiedBytes,
		UsedInodes: occupiedInodes,
	}, nil
}
