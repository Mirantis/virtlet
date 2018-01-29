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

package testing

import (
	"os/exec"
	"testing"
)

type TestCommand struct {
	t       *testing.T
	Command *exec.Cmd
}

// RunProcess runs a background process with specified args and
// appending env to the current environment.
func RunProcess(t *testing.T, command string, args []string, env []string) *TestCommand {
	cmd := exec.Command(command, args...)
	if env != nil {
		cmd.Env = env
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Error when starting the command %q: %v", command, err)
	}
	return &TestCommand{t, cmd}
}

func (tc *TestCommand) Stop() {
	if err := tc.Command.Process.Kill(); err != nil {
		tc.t.Errorf("failed to kill the child process: %v", err)
	}

	err := tc.Command.Wait()
	if err == nil {
		return
	}
	if _, ok := err.(*exec.ExitError); ok {
		return
	}
	tc.t.Errorf("Wait() failed: %v", err)
}

func (tc *TestCommand) Pid() int {
	return tc.Command.Process.Pid
}
