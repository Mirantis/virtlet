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
	"strings"
	"testing"

	"github.com/Mirantis/virtlet/tests/gm"
)

func TestGenCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
	}{
		{
			name: "plain",
			args: "",
		},
		{
			name: "dev",
			args: "--dev",
		},
		{
			name: "compat",
			args: "--compat",
		},
		{
			name: "compat dev",
			args: "--compat --dev",
		},
		{
			name: "tag",
			args: "--tag 0.9.42",
		},
		{
			name: "crd",
			args: "--crd",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := NewGenCmd(&out)
			if tc.args != "" {
				cmd.SetArgs(strings.Split(tc.args, " "))
			}
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			if err := cmd.Execute(); err != nil {
				t.Errorf("command failed: %q: %v", tc.args, err)
			} else {
				gm.Verify(t, gm.NewYamlVerifier(out.Bytes()))
			}
		})
	}
}
