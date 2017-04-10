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

package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/golang/glog"
)

// DownloadFile saves pointed by protocol/location raw data under fileName in /tmp
func DownloadFile(protocol, location, fileName string) (string, error) {
	url := fmt.Sprintf("%s://%s", protocol, location)

	path := "/tmp/" + fileName
	fp, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer fp.Close()

	glog.V(2).Infof("Start downloading %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	_, err = io.Copy(fp, resp.Body)
	if err != nil {
		return "", err
	}
	glog.V(2).Infof("Data from url %s saved in %s", url, path)
	return path, nil
}
