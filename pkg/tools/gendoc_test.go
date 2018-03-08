/*
Copyright 2018 Mirantis

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

package tools

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mirantis/virtlet/tests/gm"
)

func TestGenDocCommand(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "gendoc-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := NewGenDocCmd(fakeCobraCommand())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{tmpDir})
	if err := cmd.Execute(); err != nil {
		t.Errorf("Error running gendoc command: %v", err)
	}

	var buf bytes.Buffer
	files, err := filepath.Glob(filepath.Join(tmpDir, "*"))
	if err != nil {
		t.Fatalf("Glob(): %v", err)
	}
	for _, p := range files {
		if _, err := fmt.Fprintf(&buf, "# FILE: %s\n", filepath.Base(p)); err != nil {
			t.Errorf("Error writing filename: %v", err)
		}
		if content, err := ioutil.ReadFile(p); err != nil {
			t.Errorf("Can't read file %q: %v", p, err)
		} else {
			if _, err := fmt.Fprintf(&buf, "%s\n", content); err != nil {
				t.Errorf("failed to write file content: %v", err)
			}
		}
	}
	gm.Verify(t, buf.Bytes())
}
