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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/golang/glog"
)

// GitDiff does 'git diff' on the specified file, first changing
// to its directory. It returns the diff and error, if any
func GitDiff(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("can't get abs path for %q: %v", path, err)
	}
	origWd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("can't get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			glog.Warning("can't chdir back to the old work dir: %v", err)
		}
	}()
	fileDir := filepath.Dir(absPath)
	if err := os.Chdir(fileDir); err != nil {
		return "", fmt.Errorf("can't chdir to %q: %v", fileDir, err)
	}
	basePath := filepath.Base(absPath)

	// https://stackoverflow.com/questions/2405305/how-to-tell-if-a-file-is-git-tracked-by-shell-exit-code
	out, err := exec.Command("git", "ls-files", "--error-unmatch", "--", basePath).CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			content, err := ioutil.ReadFile(absPath)
			if err != nil {
				return "", fmt.Errorf("error reading file %q: %v", absPath, err)
			}
			return "<NEW FILE>\n" + string(content), nil
		}
		return "", err
	}

	out, err = exec.Command("git", "diff", "--", basePath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff failed on %q: %v", basePath, err)
	}
	return string(out), nil
}
