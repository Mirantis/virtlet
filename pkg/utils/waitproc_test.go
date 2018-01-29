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
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

func verifyWaitProc(t *testing.T, tmpDir string) {
	procFile := filepath.Join(tmpDir, "sample.proc")
	tc := testutils.RunProcess(t, "/bin/sh", []string{
		"-c",
		fmt.Sprintf("sleep 0.5 && echo \"$$ `cut -d' ' -f22 /proc/$$/stat`\" > '%s' && sleep 1000", procFile),
	}, nil)
	defer tc.Stop()

	pid, err := WaitForProcess(procFile)
	if err != nil {
		t.Fatalf("WaitForProcess(): %v", err)
	} else if pid != tc.Pid() {
		t.Errorf("bad pid returned by WaitForProcess: %d instead of %d", pid, tc.Pid())
	}
}

func TestWaitProc(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WaitForProcess only works on Linux")
	}
	tmpDir, err := ioutil.TempDir("", "waitproc-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for i := 0; i < 3; i++ {
		verifyWaitProc(t, tmpDir)
	}
}
