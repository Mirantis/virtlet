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

package gm

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
)

const (
	dataIndent = "  "
)

var badFilenameCharRx = regexp.MustCompile(`[^\w-]`)

// GetFilenameForTest convers a Go test name to filename
func GetFilenameForTest(testName string) (string, error) {
	filename := strings.Replace(testName, "/", "__", -1)
	filename = badFilenameCharRx.ReplaceAllString(filename, "_") + ".json"
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("can't get current directory: %v", err)
	}
	return filepath.Join(wd, filename), nil
}

// WriteDataFile serializes the specified value into a data file
func WriteDataFile(filename string, v interface{}) error {
	out, err := json.MarshalIndent(v, "", dataIndent)
	if err != nil {
		return fmt.Errorf("failed to marshal data for %q: %v. Input:\n%s",
			filename, err, spew.Sdump(v))
	}
	if err := ioutil.WriteFile(filename, out, 0777); err != nil {
		return fmt.Errorf("error writing %q: %v", filename, err)
	}
	return nil
}

// DataFileDiffers compares the specified value against the stored data file
func DataFileDiffers(filename string, v interface{}) (bool, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("error reading %q: %v", filename, err)
	}

	var curData interface{}
	if err := json.Unmarshal(content, &curData); err != nil {
		glog.Warningf("Failed to unmarshal %q to JSON: %v", filename, err)
		return true, nil
	}

	out, err := json.Marshal(v)
	if err != nil {
		return false, fmt.Errorf("failed to marshal data for %q: %v. Input:\n%s",
			filename, err, spew.Sdump(v))
	}

	var newData interface{}
	if err := json.Unmarshal(out, &newData); err != nil {
		return false, fmt.Errorf("failed to unmarshal back marshalled value for %q: %v. JSON:\n%s\nOriginal data:\n%s",
			filename, err, string(out), spew.Sdump(v))
	}

	return !reflect.DeepEqual(curData, newData), nil
}
