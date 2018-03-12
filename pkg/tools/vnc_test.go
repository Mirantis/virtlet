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
)

func TestVNCCommand(t *testing.T) {
	for _, tc := range []struct {
		args                 string
		expectedCommands     map[string]string
		expectedPortForwards []string
		expectedOutput       string
		errSubstring         string
	}{
		{
			args: "cirros",
			expectedCommands: map[string]string{
				"virtlet-foo42/libvirt/kube-system: virsh domdisplay virtlet-cc349e91-dcf7-foocontainer": "vnc://127.0.0.1:0",
			},
			expectedPortForwards: []string{
				"virtlet-foo42/kube-system: :5900",
			},
			expectedOutput: "Press ctrl-c",
		},
		{
			args:         "ubuntu",
			errSubstring: "can't get VM pod info",
		},
	} {
		t.Run(tc.args, func(t *testing.T) {
			c := &fakeKubeClient{
				t: t,
				virtletPods: map[string]string{
					"kube-node-1": "virtlet-foo42",
				},
				vmPods: map[string]VMPodInfo{
					"cirros": {
						NodeName:       "kube-node-1",
						VirtletPodName: "virtlet-foo42",
						ContainerID:    "cc349e91-dcf7-4f11-a077-36c3673c3fc4",
						ContainerName:  "foocontainer",
					},
				},
				expectedCommands:     tc.expectedCommands,
				expectedPortForwards: tc.expectedPortForwards,
			}
			var out bytes.Buffer
			cmd := NewVNCCmd(c, &out, false)
			cmd.SetArgs(strings.Split(tc.args, " "))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			switch err := cmd.Execute(); {
			case err != nil && tc.errSubstring == "":
				t.Errorf("virsh command returned an unexpected error: %v", err)
			case err == nil && tc.errSubstring != "":
				t.Errorf("Didn't get expected error (substring %q), output: %q", tc.errSubstring, out.String())
			case err != nil && !strings.Contains(err.Error(), tc.errSubstring):
				t.Errorf("Didn't get expected substring %q in the error: %v", tc.errSubstring, err)
			case err == nil && !strings.Contains(out.String(), tc.expectedOutput):
				t.Errorf("Unexpected output from the command: %q instead of %q", out.String(), tc.expectedOutput)
			}
			for c := range tc.expectedCommands {
				t.Errorf("command not executed: %q", c)
			}
		})
	}
}
