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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	expectedImageSize = 10485760
)

// Here's how this test was made:
// $ qemu-img create -f qcow2 /tmp/foobar.qcow2 10M
// Formatting '/tmp/foobar.qcow2', fmt=qcow2 size=10485760 encryption=off cluster_size=65536 lazy_refcounts=off refcount_bits=16
// $ qemu-img info --output json /tmp/foobar.qcow2
// {
// 	"virtual-size": 10485760,
// 	"filename": "/tmp/foobar.qcow2",
// 	"cluster-size": 65536,
// 	"format": "qcow2",
// 	"actual-size": 200704,
// 	"format-specific": {
// 		"type": "qcow2",
// 		"data": {
// 			"compat": "1.1",
// 			"lazy-refcounts": false,
// 			"refcount-bits": 16,
// 			"corrupt": false
// 		}
// 	},
// 	"dirty-flag": false
// }

func TestImageSize(t *testing.T) {
	// it may be possible to run it on non-Linux systems but
	// that would require installing qemu-img tools
	if runtime.GOOS != "linux" {
		t.Skip("ImageSize only works on Linux")
	}

	tmpDir, err := ioutil.TempDir("", "images")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	imagePath := filepath.Join(tmpDir, "image.qcow2")
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", imagePath, "10M").CombinedOutput(); err != nil {
		t.Fatalf("qemu-img create: %q: %v\noutput:\n%v", imagePath, err, out)
	}

	imageSize, err := GetImageVirtualSize(imagePath)
	if err != nil {
		t.Fatalf("GetImageSize(): %q: %v", imagePath, err)
	}

	if imageSize != expectedImageSize {
		t.Errorf("bad image size: %d instead of %d", imageSize, expectedImageSize)
	}
}
