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
	"testing"
)

func VerifyNamed(t *testing.T, name string, data interface{}) {
	testName := t.Name()
	if name != "" {
		testName += "__" + name
	}
	filename, err := GetFilenameForTest(testName)
	if err != nil {
		t.Errorf("can't get filename for test %q: %v", testName, err)
		return
	}
	hasDiff, err := DataFileDiffers(filename, data)
	if err != nil {
		t.Errorf("failed to diff data file %q: %v", filename, err)
		return
	}
	if hasDiff {
		if err := WriteDataFile(filename, data); err != nil {
			t.Errorf("failed to write file %q: %v", filename, data)
		}
	}

	gitDiff, err := GitDiff(filename)
	switch {
	case err != nil:
		t.Errorf("git diff failed on %q: %v", filename, err)
	case gitDiff == "":
		// no difference
	default:
		t.Errorf("got difference for %q:\n%s", testName, gitDiff)
	}
}

func Verify(t *testing.T, data interface{}) {
	VerifyNamed(t, "", data)
}
