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
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestFileNaming(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}
	for _, tc := range []struct{ testName, filename string }{
		{
			"TestSomething",
			"TestSomething.json",
		},
		{
			"TestSomething/qqq",
			"TestSomething__qqq.json",
		},
		{
			"TestSomething/it's_foobar",
			"TestSomething__it_s_foobar.json",
		},
	} {
		filename, err := GetFilenameForTest(tc.testName)
		if err != nil {
			t.Errorf("GetFilenameForTest failed for %q: %v", tc.testName, err)
		}
		if !strings.HasPrefix(filename, wd+"/") {
			t.Errorf("%q: the filename doesn't have workdir prefix: %q (workdir: %q)", tc.testName, filename, wd)
			continue
		}
		filename = filename[len(wd)+1:]
		if filename != tc.filename {
			t.Errorf("bad filename: %q instead of %q", tc.testName, tc.filename)
		}
	}
}

func TestData(t *testing.T) {
	f, err := ioutil.TempFile("", "gm-test-")
	if err != nil {
		t.Fatalf("ioutil.TempFile(): %v", err)
	}
	tmpFile := f.Name()
	defer os.Remove(tmpFile)
	if err := f.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	hasDiff, err := DataFileDiffers(tmpFile, map[string]interface{}{"x": 42})
	switch {
	case err != nil:
		t.Errorf("DataFileDiffers failed on a non-existing file: %v", err)
	case !hasDiff:
		t.Errorf("got no diff for non-existing file")
	}

	if err := WriteDataFile(tmpFile, map[string]interface{}{"x": 42}); err != nil {
		t.Fatalf("WriteDataFile(): %v", err)
	}

	hasDiff, err = DataFileDiffers(tmpFile, map[string]interface{}{"x": 42})
	switch {
	case err != nil:
		t.Errorf("DataFileDiffers failed on an unchanged file: %v", err)
	case hasDiff:
		t.Errorf("got diff for unchanged file")
	}

	hasDiff, err = DataFileDiffers(tmpFile, map[string]interface{}{"x": 43})
	switch {
	case err != nil:
		t.Errorf("DataFileDiffers failed on a changed file: %v", err)
	case !hasDiff:
		t.Errorf("didn't get diff for a changed file")
	}

	if err := ioutil.WriteFile(tmpFile, []byte(`{   "x":   42}  `), 0777); err != nil {
		t.Fatalf("error writing %q: %v", tmpFile, err)
	}

	hasDiff, err = DataFileDiffers(tmpFile, map[string]interface{}{"x": 42})
	switch {
	case err != nil:
		t.Errorf("DataFileDiffers failed: %v", err)
	case hasDiff:
		t.Errorf("got diff for unchanged file with different json formatting")
	}

	if err := ioutil.WriteFile(tmpFile, []byte(`{   `), 0777); err != nil {
		t.Fatalf("error writing %q: %v", tmpFile, err)
	}

	hasDiff, err = DataFileDiffers(tmpFile, map[string]interface{}{"x": 42})
	switch {
	case err != nil:
		t.Errorf("DataFileDiffers failed on a malformed file: %v", err)
	case !hasDiff:
		t.Errorf("got no diff for malformed target file")
	}
}
