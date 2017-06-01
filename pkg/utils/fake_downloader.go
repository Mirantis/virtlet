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
	"path/filepath"
)

type fakeDownloader struct {
	baseDir string
}

// NewFakeDownloader returns a fake downloader that places fake
// downloaded files under the specified dir. The fake downloader
// writes the location passed to it into the file instead of actually
// downloading it.
func NewFakeDownloader(baseDir string) *fakeDownloader {
	return &fakeDownloader{baseDir}
}

func (d *fakeDownloader) DownloadFile(location string) (string, error) {
	filename := NewUuid5("e8a52768-229e-4d59-830b-9ec40ba76e70", location)
	fullPath := filepath.Join(d.baseDir, filename)
	if err := ioutil.WriteFile(fullPath, []byte(location), 0777); err != nil {
		return "", err
	}
	return fullPath, nil
}
