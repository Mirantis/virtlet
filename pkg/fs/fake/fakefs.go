/*
Copyright 2019 Mirantis

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

package fake

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mirantis/virtlet/pkg/fs"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

type fakeDelimitedReader struct {
	rec      testutils.Recorder
	fileData string
}

var _ fs.DelimitedReader = &fakeDelimitedReader{}

// ReadString implements ReadString method of utils.FileReader interface
func (fr *fakeDelimitedReader) ReadString(delim byte) (line string, err error) {
	lines := strings.SplitN(fr.fileData, string(delim), 1)
	line = lines[0]
	if len(lines) > 1 {
		fr.fileData = lines[1]
	} else {
		err = io.EOF
	}
	fr.rec.Rec("ReadString", line)
	return
}

// Close implements Close method of utils.FileReader interface
func (fr *fakeDelimitedReader) Close() error {
	return nil
}

// FakeFileSystem is a fake implementation of FileSystem interface
// that uses a Recorder to record the operations performed on it.
type FakeFileSystem struct {
	t              *testing.T
	rec            testutils.Recorder
	mountParentDir string
	files          map[string]string
}

var _ fs.FileSystem = &FakeFileSystem{}

// NewFakeFileSystem creates a new instance of FakeFileSystem using
// the provided recorder and a directory that should be parent for all
// the fake mountpoints. It also takes map with fake files that will
// be accessible with GetDelimitedReader (besides those written with
// WriteFile).
func NewFakeFileSystem(t *testing.T, rec testutils.Recorder, mountParentDir string, files map[string]string) *FakeFileSystem {
	return &FakeFileSystem{t: t, rec: rec, mountParentDir: mountParentDir, files: files}
}

func (fs *FakeFileSystem) validateMountPath(target string) {
	if fs.mountParentDir == "" || filepath.Dir(target) != filepath.Clean(fs.mountParentDir) {
		fs.t.Fatalf("bad path encountered by the fs: %q (mountParentDir %q)", target, fs.mountParentDir)
	}
}

// Mount implements the Mount method of FileSystem interface.
func (fs *FakeFileSystem) Mount(source string, target string, fstype string, bind bool) error {
	fs.validateMountPath(target)
	fs.rec.Rec("Mount", []interface{}{source, target, fstype, bind})

	// We want to check directory contents both before & after mount,
	// see comment in FlexVolumeDriver.mount() in flexvolume.go.
	// So we move the original contents to .shadowed subdir.
	shadowedPath := filepath.Join(target, ".shadowed")
	if err := os.Mkdir(shadowedPath, 0755); err != nil {
		fs.t.Fatalf("os.Mkdir(): %v", err)
	}

	pathsToShadow, err := filepath.Glob(filepath.Join(target, "*"))
	if err != nil {
		fs.t.Fatalf("filepath.Glob(): %v", err)
	}
	for _, pathToShadow := range pathsToShadow {
		filename := filepath.Base(pathToShadow)
		if filename == ".shadowed" {
			continue
		}
		if err := os.Rename(pathToShadow, filepath.Join(shadowedPath, filename)); err != nil {
			fs.t.Fatalf("os.Rename(): %v", err)
		}
	}
	return nil
}

// Unmount implements the Unmount method of FileSystem interface.
func (fs *FakeFileSystem) Unmount(target string, detach bool) error {
	// we make sure that path is under our tmpdir before wiping it
	fs.validateMountPath(target)
	fs.rec.Rec("Unmount", []interface{}{target, detach})

	paths, err := filepath.Glob(filepath.Join(target, "*"))
	if err != nil {
		fs.t.Fatalf("filepath.Glob(): %v", err)
	}
	for _, path := range paths {
		if filepath.Base(path) != ".shadowed" {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			fs.t.Fatalf("os.RemoveAll(): %v", err)
		}
	}

	// We don't clean up '.shadowed' dir here because flexvolume driver
	// recursively removes the whole dir tree anyway.
	return nil
}

// IsPathAnNs implements the IsPathAnNs method of FileSystem interface.
func (fs *FakeFileSystem) IsPathAnNs(path string) bool {
	return false
}

// ChownForEmulator implements ChownForEmulator method of FileSystem interface.
func (fs *FakeFileSystem) ChownForEmulator(filePath string, recursive bool) error {
	fs.rec.Rec("ChownForEmulator", []interface{}{filePath, recursive})
	return nil
}

// GetDelimitedReader implements the FileReader method of FileSystem interface.
func (fs *FakeFileSystem) GetDelimitedReader(path string) (fs.DelimitedReader, error) {
	data, ok := fs.files[path]
	if !ok {
		fs.rec.Rec("GetDelimitedReader", fmt.Sprintf("undefined path %q", path))
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("file not found")}
	}
	fs.rec.Rec("GetDelimitedReader", path)
	return &fakeDelimitedReader{rec: fs.rec, fileData: data}, nil
}

// WriteFile implements the WriteFile method of FilesManipulator interface.
func (fs *FakeFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	fs.rec.Rec("WriteFile", []interface{}{path, string(data)})
	fs.files[path] = string(data)
	return nil
}
