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
	for _, tc := range []struct {
		testName string
		filename string
		data     interface{}
	}{
		{
			"TestSomething",
			"TestSomething.out.json",
			[]string{"foobar"},
		},
		{
			"TestSomething/qqq",
			"TestSomething__qqq.out.json",
			[]string{"foobar"},
		},
		{
			"TestSomething/it's_foobar",
			"TestSomething__it_s_foobar.out.json",
			[]string{"foobar"},
		},
		{
			"TestSomething",
			"TestSomething.out.txt",
			"foobar",
		},
		{
			"TestSomething",
			"TestSomething.out.yaml",
			NewYamlVerifier([]string{"foobar"}),
		},
	} {
		filename, err := GetFilenameForTest(tc.testName, tc.data)
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
	for _, tc := range []struct {
		name        string
		toWrite     interface{}
		toWriteRaw  string
		compareWith interface{}
		diff        bool
	}{
		{
			name:        "non-existent file",
			compareWith: map[string]interface{}{"x": 42},
			diff:        true,
		},
		{
			name:        "unchanged",
			toWrite:     map[string]interface{}{"x": 42},
			compareWith: map[string]interface{}{"x": 42},
			diff:        false,
		},
		{
			name:        "changed",
			toWrite:     map[string]interface{}{"x": 42},
			compareWith: map[string]interface{}{"x": 43},
			diff:        true,
		},
		{
			name:        "unchanged with different formatting",
			toWriteRaw:  `{   "x":   42}  `,
			compareWith: map[string]interface{}{"x": 42},
			diff:        false,
		},
		{
			name:        "malformed golden master data file",
			toWriteRaw:  `{   `,
			compareWith: map[string]interface{}{"x": 42},
			diff:        true,
		},
		{
			name:        "text content",
			toWrite:     "abcdef",
			compareWith: "abcdef",
			diff:        false,
		},
		{
			name:        "text content with changes",
			toWrite:     "abcdef",
			compareWith: "abcdefgh",
			diff:        true,
		},
		{
			name:        "text content (raw)",
			toWriteRaw:  "abcdef",
			compareWith: "abcdef",
			diff:        false,
		},
		{
			name:        "yaml content",
			toWrite:     NewYamlVerifier(map[string]interface{}{"x": 42}),
			compareWith: NewYamlVerifier(map[string]interface{}{"x": 42}),
			diff:        false,
		},
		{
			name:        "yaml content with changes",
			toWrite:     NewYamlVerifier(map[string]interface{}{"x": 42}),
			compareWith: NewYamlVerifier(map[string]interface{}{"x": 43}),
			diff:        true,
		},
		{
			name:        "yaml content (raw)",
			toWriteRaw:  "x: 42",
			compareWith: NewYamlVerifier(map[string]interface{}{"x": 42}),
			diff:        false,
		},
		{
			name:        "yaml content (raw comparison, string)",
			toWriteRaw:  "x: 42",
			compareWith: NewYamlVerifier("x: 42"),
			diff:        false,
		},
		{
			name:        "yaml content with changes (raw comparison, string)",
			toWriteRaw:  "x: 42",
			compareWith: NewYamlVerifier("x: 43"),
			diff:        true,
		},
		{
			name:        "yaml content (raw comparison, []byte)",
			toWriteRaw:  "x: 42",
			compareWith: NewYamlVerifier([]byte("x: 42")),
			diff:        false,
		},
		{
			name:        "yaml content with changes (raw comparison, []byte)",
			toWriteRaw:  "x: 42",
			compareWith: NewYamlVerifier([]byte("x: 43")),
			diff:        true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := ioutil.TempFile("", "gm-test-")
			if err != nil {
				t.Fatalf("ioutil.TempFile(): %v", err)
			}
			tmpFile := f.Name()
			defer os.Remove(tmpFile)
			if err := f.Close(); err != nil {
				t.Fatalf("Close(): %v", err)
			}

			if tc.toWrite != nil {
				if err := WriteDataFile(tmpFile, tc.toWrite); err != nil {
					t.Fatalf("WriteDataFile(): %v", err)
				}
			} else if tc.toWriteRaw != "" {
				if err := ioutil.WriteFile(tmpFile, []byte(tc.toWriteRaw), 0777); err != nil {
					t.Fatalf("error writing %q: %v", tmpFile, err)
				}
			}

			hasDiff, err := DataFileDiffers(tmpFile, tc.compareWith)
			switch {
			case err != nil:
				t.Errorf("DataFileDiffers failed: %v", err)
			case tc.diff && !hasDiff:
				t.Errorf("diff expected but got no diff")
			case !tc.diff && hasDiff:
				t.Errorf("no diff expected but got diff")
			}
		})

	}
}
