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

package fs

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

const sampleMountInfo = `28 0 252:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw,data=ordered
25 28 0:6 / /dev rw,nosuid,relatime shared:2 - devtmpfs udev rw,size=16457220k,nr_inodes=4114305,mode=755
26 25 0:23 / /dev/pts rw,nosuid,noexec,relatime shared:3 - devpts devpts rw,gid=5,mode=620,ptmxmode=000
27 28 0:24 / /run rw,nosuid,noexec,relatime shared:5 - tmpfs tmpfs rw,size=3293976k,mode=755
632 27 0:3 net:[4026532228] PATH/run/docker/netns/5d119181c6d0 rw shared:195 - nsfs nsfs rw
671 27 0:3 net:[4026532301] PATH/run/docker/netns/421c937a8f90 rw shared:199 - nsfs nsfs rw
`

func TestFileSystem(t *testing.T) {
	// FIXME: this test is not comprehensive enough right now
	tmpDir, err := ioutil.TempDir("", "fs-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	realTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("Can't get the real path of %q: %v", tmpDir, err)
	}
	defer os.RemoveAll(tmpDir)

	mountInfoPath := filepath.Join(tmpDir, "mountinfo")
	mountInfo := strings.Replace(sampleMountInfo, "PATH", realTmpDir, -1)
	if err := ioutil.WriteFile(mountInfoPath, []byte(mountInfo), 0644); err != nil {
		t.Fatalf("ioutil.WriteFile(): %v", err)
	}

	fs := realFileSystem{mountInfoPath: mountInfoPath}
	sampleFilePath := filepath.Join(tmpDir, "foobar")
	if err := fs.WriteFile(sampleFilePath, []byte("foo\nbar\n"), 0777); err != nil {
		t.Fatalf("fs.WriteFile(): %v", err)
	}
	r, err := fs.GetDelimitedReader(sampleFilePath)
	if err != nil {
		t.Fatalf("GetDelimitedReader(): %v", err)
	}

	for _, expected := range []string{"foo\n", "bar\n"} {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString(): %v", err)
		}
		if line != expected {
			t.Errorf("Bad line 1: %q instead of %q", line, expected)
		}
	}

	_, err = r.ReadString('\n')
	switch err {
	case nil:
		t.Errorf("Didn't get an io.EOF error")
	case io.EOF:
		// ok
	default:
		t.Errorf("Wrong error type at EOF: %T: %v", err, err)
	}

	for _, tc := range []struct {
		path string
		isNs bool
	}{
		{
			path: "run/docker/netns/5d119181c6d0",
			isNs: true,
		},
		{
			path: "run/docker/netns/421c937a8f90",
			isNs: true,
		},
		{
			path: "run",
			isNs: false,
		},
		{
			path: "etc",
			isNs: false,
		},
	} {
		path := filepath.Join(realTmpDir, tc.path)
		if err := os.MkdirAll(path, 0777); err != nil {
			t.Fatalf("MkdirAll(): %v", err)
		}

		isNs := fs.IsPathAnNs(path)
		if isNs != tc.isNs {
			t.Errorf("IsPathAnNs(%q) = %v but expected to be %v", path, isNs, tc.isNs)
		}
	}
	// TODO: when running in a build container, also test ChownForEmulator and mounting
}

func TestFileUtils(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "genisoimage-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := WriteFiles(tmpDir, map[string][]byte{
		"image.cd/file1.txt":       []byte("foo"),
		"image.cd/anotherfile.txt": []byte("bar"),
	}); err != nil {
		t.Fatalf("WriteFiles(): %v", err)
	}

	isoPath := filepath.Join(tmpDir, "image.iso")
	srcPath := filepath.Join(tmpDir, "image.cd")
	if err := GenIsoImage(isoPath, "isoimage", srcPath); err != nil {
		t.Fatalf("GenIsoImage(): %v", err)
	}

	m, err := testutils.IsoToMap(isoPath)
	if err != nil {
		t.Fatalf("IsoToMap(): %v", err)
	}
	expectedFiles := map[string]interface{}{
		"file1.txt":       "foo",
		"anotherfile.txt": "bar",
	}
	if !reflect.DeepEqual(m, expectedFiles) {
		t.Errorf("bad iso content: %#v instead of %#v", m, expectedFiles)
	}
}
