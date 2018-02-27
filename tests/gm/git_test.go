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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	gitSetupCmd = `
git config --global user.email 'foo@example.com' &&
git config --global user.name 'foo' &&
git init &&
git add . &&
git commit -m 'ok'`
)

func TestGitDiff(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "git-test")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("os.Chdir(): %v", err)
	}
	if err := ioutil.WriteFile("samplefile", []byte("foobar"), 0777); err != nil {
		t.Fatalf("ioutil.WriteFile(): %v", err)
	}

	out, err := exec.Command("/bin/sh", "-c", gitSetupCmd).CombinedOutput()
	if err != nil {
		t.Fatalf("git init failed: %v:\n%s", err, out)
	}

	diffOut, err := GitDiff(filepath.Join(tmpDir, "samplefile"))
	switch {
	case err != nil:
		t.Errorf("GitDiff (unchanged): %v", err)
	case diffOut != "":
		t.Errorf("GitDiff (unchanged): unexpected diff:\n%s", diffOut)
	}

	if err := ioutil.WriteFile("samplefile", []byte("baz"), 0777); err != nil {
		t.Fatalf("ioutil.WriteFile(): %v", err)
	}

	diffOut, err = GitDiff(filepath.Join(tmpDir, "samplefile"))
	switch {
	case err != nil:
		t.Errorf("GitDiff (changed): %v", err)
	case diffOut == "":
		t.Errorf("GitDiff (changed): no diff")
	case !strings.Contains(diffOut, "-foobar") || !strings.Contains(diffOut, "+baz"):
		t.Errorf("GitDiff (changed): bad diff output:\n%s", diffOut)
	}

	out, err = exec.Command("git", "add", "samplefile").CombinedOutput()
	if err != nil {
		t.Fatalf("git add failed: %v:\n%s", err, out)
	}

	diffOut, err = GitDiff(filepath.Join(tmpDir, "samplefile"))
	switch {
	case err != nil:
		t.Errorf("GitDiff (staged): %v", err)
	case diffOut != "":
		t.Errorf("GitDiff (staged): unexpected diff:\n%s", diffOut)
	}

	diffOut, err = GitDiff(filepath.Join(tmpDir, "samplefile"))
	switch {
	case err != nil:
		t.Errorf("GitDiff (staged, other work dir): %v", err)
	case diffOut != "":
		t.Errorf("GitDiff (staged, other work dir): unexpected diff:\n%s", diffOut)
	}

	if err := ioutil.WriteFile("newfile", []byte("newcontent"), 0777); err != nil {
		t.Fatalf("ioutil.WriteFile(): %v", err)
	}

	diffOut, err = GitDiff(filepath.Join(tmpDir, "newfile"))
	switch {
	case err != nil:
		t.Errorf("GitDiff (new): %v", err)
	case diffOut == "":
		t.Errorf("GitDiff (new): no diff")
	case !strings.Contains(diffOut, "<NEW FILE>") || !strings.Contains(diffOut, "newcontent"):
		t.Errorf("GitDiff (new): bad diff output:\n%s", diffOut)
	}
}
