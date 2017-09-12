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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/imagetranslation"
)

// Downloader is an interface for downloading files from web
type Downloader interface {
	// DownloadFile downloads the specified file and returns path
	// to it
	DownloadFile(endpoint imagetranslation.Endpoint) (string, error)
}

type defaultDownloader struct {
	protocol string
}

// NewDownloader returns the default downloader for 'protocol'.
// The default downloader downloads a file via an URL constructed as
// 'protocol://location' and saves it in temporary file in default
// system directory for temporary files
func NewDownloader(protocol string) Downloader {
	return &defaultDownloader{protocol}
}

func (d *defaultDownloader) DownloadFile(endpoint imagetranslation.Endpoint) (string, error) {
	url := endpoint.Url
	if !strings.Contains(url, "://") {
		url = fmt.Sprintf("%s://%s", d.protocol, url)
	}

	tempFile, err := ioutil.TempFile("", "virtlet_")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	glog.V(2).Infof("Start downloading %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	_, err = io.Copy(tempFile, resp.Body)
	if err != nil {
		return "", err
	}
	glog.V(2).Infof("Data from url %s saved in %s", url, tempFile.Name())
	return tempFile.Name(), nil
}
