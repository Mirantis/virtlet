// +build !linux

/*
Copyright 2016 Mirantis

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
	"errors"
)

// FormatDisk is a stub for non-linux systems
func FormatDisk(path string) error {
	return errors.New("not implemented")
}

// Put is a stub for non-linux systems
func Put(image string, files map[string][]byte) error {
	return errors.New("not implemented")
}

// ListFiles is a stub for non-linux systems
func ListFiles(imagePath, dir string) ([]string, error) {
	return nil, errors.New("not implemented")
}

// Cat is a stub for non-linux systems
func Cat(imagePath, filePath string) (string, error) {
	return "", errors.New("not implemented")
}
