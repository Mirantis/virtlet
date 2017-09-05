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
	"os/exec"
	"path/filepath"
	"testing"
)

func verifyWaitProc(t *testing.T, tmpDir string) {
	procFile := filepath.Join(tmpDir, "sample.proc")
	cmd := exec.Command("/bin/sh", "-c",
		fmt.Sprintf("sleep 0.5 && echo \"$$ `cut -d' ' -f22 /proc/$$/stat`\" > '%s' && sleep 1000", procFile))
	if err := cmd.Start(); err != nil {
		t.Fatalf("couldn't run command: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("couldn't kill the process")
		}
		cmd.Wait()
	}()

	pid, err := WaitForProcess(procFile)
	if err != nil {
		t.Fatalf("WaitForProcess(): %v", err)
	} else if pid != cmd.Process.Pid {
		t.Errorf("bad pid returned by WaitForProcess: %d instead of %d", pid, cmd.Process.Pid)
	}
}

func TestWaitProc(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "waitproc-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for i := 0; i < 3; i++ {
		verifyWaitProc(t, tmpDir)
	}
}
