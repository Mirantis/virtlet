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

func TestVirshCommand(t *testing.T) {
	for _, tc := range []struct {
		args             string
		expectedCommands map[string]string
		expectedOutput   string
		errSubstring     string
	}{
		{
			args: "list --node=kube-node-1",
			expectedCommands: map[string]string{
				"virtlet-foo42/libvirt/kube-system: virsh list": "foobar",
			},
			expectedOutput: "foobar",
		},
		{
			args: "list",
			expectedCommands: map[string]string{
				"virtlet-foo42/libvirt/kube-system: virsh list": "foobar",
				"virtlet-bar42/libvirt/kube-system: virsh list": "baz",
			},
			expectedOutput: "*** node: kube-node-1 pod: virtlet-foo42 ***\n" +
				"foobar\n" +
				"*** node: kube-node-2 pod: virtlet-bar42 ***\n" +
				"baz\n",
		},
		{
			args: "dumpxml @cirros",
			expectedCommands: map[string]string{
				"virtlet-foo42/libvirt/kube-system: virsh dumpxml virtlet-cc349e91-dcf7-foocontainer": "foobar",
			},
			expectedOutput: "foobar",
		},
		{
			args: "dumpxml @ubuntu",
			expectedCommands: map[string]string{
				"virtlet-bar42/libvirt/kube-system: virsh dumpxml virtlet-4707196f-1d93-vm": "foobar",
			},
			expectedOutput: "foobar",
		},
		{
			args: "dumpxml @cirros --node=kube-node-1",
			expectedCommands: map[string]string{
				"virtlet-foo42/libvirt/kube-system: virsh dumpxml virtlet-cc349e91-dcf7-foocontainer": "foobar",
			},
			expectedOutput: "foobar",
		},
		{
			args:         "dumpxml @cirros --node=kube-node-2",
			errSubstring: "--node specifies a node other than one that runs the VM pod",
		},
		{
			args: "whatever @cirros @cirros1",
			expectedCommands: map[string]string{
				"virtlet-foo42/libvirt/kube-system: virsh whatever virtlet-cc349e91-dcf7-foocontainer virtlet-68e6fede-aab2-qq": "foobar",
			},
			expectedOutput: "foobar",
		},
		{
			args:         "whatever @cirros @ubuntu",
			errSubstring: "can't reference VM pods that run on different nodes",
		},
	} {
		t.Run(tc.args, func(t *testing.T) {
			c := &fakeKubeClient{
				t: t,
				virtletPods: map[string]string{
					"kube-node-1": "virtlet-foo42",
					"kube-node-2": "virtlet-bar42",
				},
				vmPods: map[string]VMPodInfo{
					"cirros": {
						NodeName:       "kube-node-1",
						VirtletPodName: "virtlet-foo42",
						ContainerID:    "cc349e91-dcf7-4f11-a077-36c3673c3fc4",
						ContainerName:  "foocontainer",
					},
					"cirros1": {
						NodeName:       "kube-node-1",
						VirtletPodName: "virtlet-foo42",
						ContainerID:    "68e6fede-aab2-4abe-b339-466386734ddb",
						ContainerName:  "qq",
					},
					"ubuntu": {
						NodeName:       "kube-node-2",
						VirtletPodName: "virtlet-bar42",
						ContainerID:    "4707196f-1d93-46ba-a138-b6201e13db6d",
						ContainerName:  "vm",
					},
				},
				expectedCommands: tc.expectedCommands,
			}
			var out bytes.Buffer
			cmd := NewVirshCmd(c, &out)
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
			case err == nil && out.String() != tc.expectedOutput:
				t.Errorf("Unexpected output from the command: %q instead of %q", out.String(), tc.expectedOutput)
			}
			for c := range tc.expectedCommands {
				t.Errorf("command not executed: %q", c)
			}
		})
	}
}
