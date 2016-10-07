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

package download

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/golang/glog"
)

func ParseShortName(fileUrl string) (string, error) {
	u, err := url.Parse(fileUrl)
	if err != nil {
		return "", err
	}
	segments := strings.Split(u.Path, "/")
	shortName := segments[len(segments)-1]

	return shortName, nil
}

func DownloadFile(fileUrl string) (string, string, error) {
	// TODO(nhlfr): Use SSL.
	fileUrl = fmt.Sprintf("http://%s", fileUrl)
	shortName, err := ParseShortName(fileUrl)
	if err != nil {
		return "", "", err
	}

	filepath := fmt.Sprintf("/tmp/%s", shortName)
	fp, err := os.Create(filepath)
	if err != nil {
		return "", "", err
	}
	defer fp.Close()

	glog.Infof("Start downloading %s", fileUrl)

	resp, err := http.Get(fileUrl)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	_, err = io.Copy(fp, resp.Body)
	if err != nil {
		return "", "", err
	}
	glog.Infof("Data from url %s saved in %s", fileUrl, filepath)
	return filepath, shortName, nil
}
