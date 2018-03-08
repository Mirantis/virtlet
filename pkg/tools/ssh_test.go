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
	"testing"
)

func TestSSHCommand(t *testing.T) {
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
		expectedPortForwards: []string{
			"cirros/default: :22",
		},
	}
	var out bytes.Buffer
	cmd := NewSSHCmd(c, &out, "/bin/echo")
	cmd.SetArgs([]string{"user@cirros", "--", "ls", "-l", "/"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ssh error: %v", err)
	}
	expectedCommand := "-q -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p 4242 user@127.0.0.1 ls -l /\n"
	if out.String() != expectedCommand {
		t.Errorf("bad ssh command line: %q instead of %q", out.String(), expectedCommand)
	}
	// TODO: support alternative remote ports
}

func TestSSHErrorOnNonVMPod(t *testing.T) {
	c := &fakeKubeClient{
		t: t,
		virtletPods: map[string]string{
			"kube-node-1": "virtlet-foo42",
		},
	}
	var out bytes.Buffer
	cmd := NewSSHCmd(c, &out, "/bin/echo")
	cmd.SetArgs([]string{"user@cirros", "--", "ls", "-l", "/"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Errorf("didn't get an error for non-VM pod")
	}
}
