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

package nsfix

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

func handleNsFixWithNilArg(data interface{}) (interface{}, error) {
	return 42, nil
}

func handleNsFixWithNilResult(data interface{}) (interface{}, error) {
	targetPath := data.(*string)
	return nil, os.Mkdir(filepath.Join(*targetPath, "foobar"), 0777)
}

func verifyNsFix(t *testing.T, toRun func(tmpDir string, dirs map[string]string, pids []int)) {
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

	// TEST_NSFIX is handled by init() below
	os.Setenv("TEST_NSFIX", "1")
	defer os.Setenv("TEST_NSFIX", "")

	dirA := filepath.Join(tmpDir, "a")
	dirD := filepath.Join(tmpDir, "d")
	dirs := map[string]string{
		"a": dirA,
		"b": filepath.Join(tmpDir, "b"),
		"c": filepath.Join(dirA, "c"),
		"d": dirD,
		"e": filepath.Join(dirD, "e"),
	}
	for _, p := range []string{"a", "b", "c", "d", "e"} {
		if err := os.Mkdir(dirs[p], 0777); err != nil {
			t.Fatalf("Can't create dir %q: %v", p, err)
		}
	}

	var pids []int
	for _, cmd := range []string{
		fmt.Sprintf("mount --bind %q %q && sleep 10000", dirs["a"], dirs["b"]),
		fmt.Sprintf("mount --bind %q %q && sleep 10000", dirs["d"], dirs["b"]),
	} {
		tc := testutils.RunProcess(t, "unshare", []string{
			"-m", "/bin/bash", "-c", cmd,
		}, nil)
		defer tc.Stop()
		pids = append(pids, tc.Pid())
	}

	toRun(tmpDir, dirs, pids)
}

func TestNsFix(t *testing.T) {
	verifyNsFix(t, func(tmpDir string, dirs map[string]string, pids []int) {
		var r nsFixTestRet
		if err := NewNsFixCall("nsFixTest1").
			TargetPid(pids[0]).
			Arg(nsFixTestArg{dirs["b"]}).
			SpawnInNamespaces(&r); err != nil {
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

		if err := NewNsFixCall("nsFixTest1").
			TargetPid(pids[1]).
			Arg(nsFixTestArg{dirs["b"]}).
			DropPrivs().
			SpawnInNamespaces(&r); err != nil {
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
		cmd.Env = append(os.Environ(), fmt.Sprintf("TEST_NSFIX_SWITCH=%d:%s", pids[1], dirs["b"]))
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				t.Errorf("Error rerunning the test executable: %v\nstderr:\n%s", err, exitErr.Stderr)
			} else {
				t.Errorf("Error rerunning the test executable: %v", err)
			}
		}

		outPath := filepath.Join(dirs["d"], "out")
		expectedContents := "e"
		switch outStr, err := ioutil.ReadFile(outPath); {
		case err != nil:
			t.Errorf("Error reading %q: %v", outPath, err)
		case string(outStr) != expectedContents:
			t.Errorf("bad out file contents: %q instead of %q", outStr, expectedContents)
		}
	})
}

func TestNsFixWithNilArg(t *testing.T) {
	verifyNsFix(t, func(tmpDir string, dirs map[string]string, pids []int) {
		var r int
		if err := NewNsFixCall("nsFixTestNilArg").
			TargetPid(pids[0]).
			SpawnInNamespaces(&r); err != nil {
			t.Fatalf("SpawnInNamespaces(): %v", err)
		}
		expectedResult := 42
		if r != expectedResult {
			t.Errorf("Bad result from SpawnInNamespaces(): %d instead of %d", r, expectedResult)
		}
	})
}

func TestNsFixWithNilResult(t *testing.T) {
	verifyNsFix(t, func(tmpDir string, dirs map[string]string, pids []int) {
		if err := NewNsFixCall("nsFixTestNilResult").
			TargetPid(pids[0]).
			Arg(dirs["b"]).
			SpawnInNamespaces(nil); err != nil {
			t.Fatalf("SpawnInNamespaces(): %v", err)
		}
		// dirs["a"] is bind-mounted under dirs["b"]
		if _, err := os.Stat(filepath.Join(dirs["a"], "foobar")); err != nil {
			t.Errorf("Stat(): %v", err)
		}
	})
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
		if err := NewNsFixCall("nsFixTest2").
			TargetPid(pid).
			Arg(nsFixTestArg{parts[1]}).
			SwitchToNamespaces(); err != nil {
			glog.Fatalf("SwitchToNamespaces(): %v", err)
		}
	}
	RegisterNsFixReexec("nsFixTest1", handleNsFixTest1, nsFixTestArg{})
	RegisterNsFixReexec("nsFixTest2", handleNsFixTest2, nsFixTestArg{})
	RegisterNsFixReexec("nsFixTestNilArg", handleNsFixWithNilArg, nil)
	RegisterNsFixReexec("nsFixTestNilResult", handleNsFixWithNilResult, "")
	if os.Getenv("TEST_NSFIX") != "" {
		// NOTE: this is not a recommended way to invoke
		// reexec, but may be the easiest one for testing
		HandleNsFixReexec()
	}
}
