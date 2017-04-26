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

package testing

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DirToMap converts directory to a map where keys are filenames
// without full path and values are the contents of the files. Files
// with '.iso' extensions are unpacked using 7z and then converted to
// a map using DirToMap. Directories are handled recursively. If the
// directory doesn't exist, dirToMap returns nil
func DirToMap(dir string) (map[string]interface{}, error) {
	if fi, err := os.Stat(dir); err != nil && os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("os.Stat(): %v", err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%q expected to be a directory", dir)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "/*"))
	if err != nil {
		return nil, fmt.Errorf("filepath.Glob(): %v", err)
	}
	m := map[string]interface{}{}
	for _, p := range paths {
		filename := filepath.Base(p)
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			err = fmt.Errorf("os.Stat(): %v", err)
		case fi.IsDir():
			m[filename], err = DirToMap(p)
		case strings.HasSuffix(filename, ".iso"):
			m[filename], err = IsoToMap(p)
		default:
			if bs, err := ioutil.ReadFile(p); err != nil {
				err = fmt.Errorf("ioutil.ReadFile(): %v", err)
			} else {
				m[filename] = string(bs)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

// IsoToMap converts an iso image to a map where keys are filenames
// without full path and values are the contents of the files. It does
// so by unpacking the image into a temporary directory and processing
// it with DirToMap.
func IsoToMap(isoPath string) (map[string]interface{}, error) {
	tmpDir, err := ioutil.TempDir("", "iso-out")
	if err != nil {
		return nil, fmt.Errorf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)
	out, err := exec.Command("7z", "x", "-o"+tmpDir, isoPath).CombinedOutput()
	if err != nil {
		outStr := ""
		if len(out) != 0 {
			outStr = ". Output:\n" + string(out)
		}
		return nil, fmt.Errorf("error unpacking iso: %v%s", err, outStr)
	}
	return DirToMap(tmpDir)
}
