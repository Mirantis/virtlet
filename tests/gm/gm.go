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

// VerifyNamed generates a file name based on current test name and
// the 'name' argument and compares its contents in git index (i.e.
// staged or committed if the changes are staged for the file) to
// that of of 'data' argument. The test will fail if the data
// differs. In this case the target file is updated and the user
// must stage or commit the changes to make the test pass.
// If data is string or []byte, it's compared directly to the contents
// of the file (the data are supposed to be human-readable and
// easily diffable).
// If data implements Verifier interface, the comparison is done
// by invoking it's Verify method on the contents of the file.
// Otherwise, the comparison is done based on JSON representation
// of the data ignoring any changes in JSON formatting and any
// changes introduced by the encoding/json marshalling mechanism.
// If data implements Verifier interface, the updated contents of the
// data file is generated using its Marshal() method.
// The suffix of the data file name is generated based on the
// data argument, too. For text content, ".out.txt" suffix is used.
// For json content, the suffix is ".out.json". For Verifier type,
// the suffix is ".out" concatenated with the value of the Suffix()
// method.
func VerifyNamed(t *testing.T, name string, data interface{}) {
	testName := t.Name()
	if name != "" {
		testName += "__" + name
	}
	filename, err := GetFilenameForTest(testName, data)
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
		t.Errorf("got difference for %q (%q):\n%s", testName, filename, gitDiff)
	}
}

// Verify invokes VerifyNamed with empty 'name' argument
func Verify(t *testing.T, data interface{}) {
	VerifyNamed(t, "", data)
}
