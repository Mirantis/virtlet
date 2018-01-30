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

package utils

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/golang/glog"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

type nsFixTestArg struct {
	DirPath string
}

type nsFixTestRet struct {
	Files  []string
	IsRoot bool
}

func listFiles(dirPath string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dirPath, "*"))
	if err != nil {
		return nil, fmt.Errorf("Glob(): %v", err)
	}

	var r []string
	for _, m := range matches {
		r = append(r, filepath.Base(m))
	}
	return r, nil
}

func handleNsFixTest1(data interface{}) (interface{}, error) {
	arg := data.(*nsFixTestArg)
	files, err := listFiles(arg.DirPath)
	if err != nil {
		return nil, err
	}
	return nsFixTestRet{files, os.Getuid() == 0}, nil
}

func handleNsFixTest2(data interface{}) (interface{}, error) {
	arg := data.(*nsFixTestArg)
	files, err := listFiles(arg.DirPath)
	if err != nil {
		return nil, err
	}

	contents := []byte(strings.Join(files, "\n"))
	if err := ioutil.WriteFile(filepath.Join(arg.DirPath, "out"), contents, 0666); err != nil {
		return nil, err
	}

	return nil, nil
}

func TestNsFix(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("The namespace fix only works on Linux")
	}
	if os.Getuid() != 0 {
		t.Skip("This test requires root privs")
	}

	tmpDir, err := ioutil.TempDir("", "nsfix-")
	if err != nil {
		t.Fatalf("Can't create temp dir for config image: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Chmod(tmpDir, 0755); err != nil {
		t.Fatalf("Chown(): %v", err)
	}

	dirA := filepath.Join(tmpDir, "a")
	dirB := filepath.Join(tmpDir, "b")
	dirC := filepath.Join(dirA, "c")
	dirD := filepath.Join(tmpDir, "d")
	dirE := filepath.Join(dirD, "e")
	for _, p := range []string{dirA, dirB, dirC, dirD, dirE} {
		if err := os.Mkdir(p, 0777); err != nil {
			t.Fatalf("Can't create dir %q: %v", p, err)
		}
	}

	var pids []int
	for _, cmd := range []string{
		fmt.Sprintf("mount --bind %q %q && sleep 10000", dirA, dirB),
		fmt.Sprintf("mount --bind %q %q && sleep 10000", dirD, dirB),
	} {
		tc := testutils.RunProcess(t, "unshare", []string{
			"-m", "/bin/bash", "-c", cmd,
		}, nil)
		defer tc.Stop()
		pids = append(pids, tc.Pid())
	}

	// TEST_NSFIX is handled by init() below
	os.Setenv("TEST_NSFIX", "1")
	defer os.Setenv("TEST_NSFIX", "")

	var r nsFixTestRet
	if err := SpawnInNamespaces(pids[0], "nsfixtest1", nsFixTestArg{dirB}, false, &r); err != nil {
		t.Fatalf("SpawnInNamespaces(): %v", err)
	}
	resultStr := strings.Join(r.Files, "\n")
	expectedResultStr := "c"
	if resultStr != expectedResultStr {
		t.Errorf("Bad result from SpawnInNamespaces(): %q instead of %q", resultStr, expectedResultStr)
	}
	if !r.IsRoot {
		t.Errorf("SpawnInNamespaces dropped privs when not requested to do so")
	}

	if err := SpawnInNamespaces(pids[1], "nsfixtest1", nsFixTestArg{dirB}, true, &r); err != nil {
		t.Fatalf("SpawnInNamespaces(): %v", err)
	}
	resultStr = strings.Join(r.Files, "\n")
	expectedResultStr = "e"
	if resultStr != expectedResultStr {
		t.Errorf("Bad result from SpawnInNamespaces(): %q instead of %q", resultStr, expectedResultStr)
	}
	if r.IsRoot {
		t.Errorf("SpawnInNamespaces didn't drop privs not requested to do so")
	}

	// we examine SwitchToNamespace in the same test to make
	// sure the calls don't interfere between themselves
	cmd := exec.Command(os.Args[0])
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Env = append(os.Environ(), fmt.Sprintf("TEST_NSFIX_SWITCH=%d:%s", pids[1], dirB))
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Errorf("Error rerunning the test executable: %v\nstderr:\n%s", err, exitErr.Stderr)
		} else {
			t.Errorf("Error rerunning the test executable: %v", err)
		}
	}

	outPath := filepath.Join(dirD, "out")
	expectedContents := "e"
	switch outStr, err := ioutil.ReadFile(outPath); {
	case err != nil:
		t.Errorf("Error reading %q: %v", outPath, err)
	case string(outStr) != expectedContents:
		t.Errorf("bad out file contents: %q instead of %q", outStr, expectedContents)
	}
}

func init() {
	if switchStr := os.Getenv("TEST_NSFIX_SWITCH"); switchStr != "" {
		os.Setenv("TEST_NSFIX_SWITCH", "")
		parts := strings.SplitN(switchStr, ":", 2)
		if len(parts) != 2 {
			glog.Fatalf("bad TEST_NSFIX_SWITCH: %q", switchStr)
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			glog.Fatalf("bad TEST_NSFIX_SWITCH: %q", switchStr)
		}
		if err := SwitchToNamespaces(pid, "nsfixtest2", nsFixTestArg{parts[1]}, false); err != nil {
			glog.Fatalf("SwitchToNamespace(): %v", err)
		}
	}
	RegisterNsFixReexec("nsfixtest1", handleNsFixTest1, nsFixTestArg{})
	RegisterNsFixReexec("nsfixtest2", handleNsFixTest2, nsFixTestArg{})
	if os.Getenv("TEST_NSFIX") != "" {
		// NOTE: this is not a recommended way to invoke
		// reexec, but may be the easiest one for testing
		HandleNsFixReexec()
	}
}
