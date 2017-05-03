/*
Copyright 2017 Mirantis

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

package utils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

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
