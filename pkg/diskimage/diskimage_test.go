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

package diskimage

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func verifyFiles(t *testing.T, imagePath, dir string, expectedFiles ...string) {
	sort.Strings(expectedFiles)
	exp := strings.Join(expectedFiles, "\n")
	files, err := ListFiles(imagePath, dir)
	if err != nil {
		t.Fatalf("ListFiles(): %v", err)
	}
	actual := strings.Join(files, "\n")
	if exp != actual {
		t.Errorf("bad file list: expected:\n%s\n-- got --\n%s", exp, actual)
	}
}

func verifyContent(t *testing.T, imagePath, filePath, exp string) {
	actual, err := Cat(imagePath, filePath)
	if err != nil {
		t.Fatalf("ListFiles(): %v", err)
	}
	if exp != actual {
		t.Errorf("bad file content for %q: expected:\n%s\n-- got --\n%s", filePath, exp, actual)
	}
}

func TestDiskImage(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("libguestfs is only supported on Linux")
	}

	tmpDir, err := ioutil.TempDir("", "diskimage")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	imagePath := filepath.Join(tmpDir, "image.qcow2")
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", imagePath, "10M").CombinedOutput(); err != nil {
		t.Fatalf("qemu-img create: %q: %v\noutput:\n%v", imagePath, err, out)
	}

	if err := FormatDisk(imagePath); err != nil {
		t.Fatalf("FormatDisk(): %v", err)
	}

	verifyFiles(t, imagePath, "/", "lost+found")

	if err := Put(imagePath, map[string][]byte{
		"/foo/bar.txt": []byte("foobar"),
		"/foo/baz.txt": []byte("baz"),
	}); err != nil {
		t.Fatalf("Put(): %v", err)
	}

	verifyFiles(t, imagePath, "/", "foo", "lost+found")
	verifyFiles(t, imagePath, "/foo", "bar.txt", "baz.txt")
	verifyContent(t, imagePath, "/foo/bar.txt", "foobar")
	verifyContent(t, imagePath, "/foo/baz.txt", "baz")

	if _, err := Cat(imagePath, "/nosuchfile"); err == nil {
		t.Errorf("didn't get the expected error")
	} else if !strings.Contains(err.Error(), "No such file or directory") {
		t.Errorf("bad error message: %q", err.Error())
	}
}
