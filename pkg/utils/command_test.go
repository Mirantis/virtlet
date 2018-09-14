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
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	for _, tc := range []struct {
		cmd            []string
		stdin          []byte
		expectedOutput string
		expectedError  string
	}{
		{
			cmd:            []string{"echo", "-n", "foobar"},
			stdin:          nil,
			expectedOutput: "foobar",
		},
		{
			cmd:            []string{"/bin/bash", "-c", "echo -n >&2 'stderr here'; echo -n 'stdout here'; exit 1"},
			stdin:          nil,
			expectedOutput: "stdout here",
			expectedError:  "stderr here",
		},
		{
			cmd:            []string{"sed", "s/foo/bar/g"},
			stdin:          []byte("here is foo; foo."),
			expectedOutput: "here is bar; bar.",
		},
	} {
		t.Run(strings.Join(tc.cmd, " "), func(t *testing.T) {
			c := DefaultCommander.Command(tc.cmd[0], tc.cmd[1:]...)
			switch out, err := c.Run(tc.stdin); {
			case tc.expectedError == "" && err != nil:
				t.Errorf("Command error: %v", err)
			case tc.expectedError != "" && err == nil:
				t.Errorf("Didn't get the expected error")
			case tc.expectedError != "" && !strings.Contains(err.Error(), tc.expectedError):
				t.Errorf("Bad error message %q (no substring %q)", err.Error(), tc.expectedError)
			case tc.expectedError != "" && !strings.Contains(err.Error(), tc.cmd[0]):
				t.Errorf("Bad error message %q (no substring %q)", err.Error(), tc.cmd[0])
			case string(out) != tc.expectedOutput:
				t.Errorf("Command output mismatch: %q instead of %q", out, tc.expectedOutput)
			}
		})
	}
}
