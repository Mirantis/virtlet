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

	"github.com/Mirantis/virtlet/pkg/version"
	"github.com/Mirantis/virtlet/tests/gm"
	"strings"
)

var (
	clientVersionInfo = version.Info{
		Major:        "1",
		Minor:        "0",
		GitVersion:   "v1.0.0-6+318faff6ad0609-dirty",
		GitCommit:    "318faff6ad060954387c1ff594bcbb4bb128577a",
		GitTreeState: "dirty",
		BuildDate:    "2018-04-16T21:02:05Z",
		GoVersion:    "go1.8.3",
		Compiler:     "gc",
		Platform:     "darwin/amd64",
		ImageTag:     "ivan4th_version",
	}
	nodeVersionInfo = version.Info{
		Major:        "1",
		Minor:        "0",
		GitVersion:   "v1.0.0-6+318faff6ad0609",
		GitCommit:    "318faff6ad060954387c1ff594bcbb4bb128577a",
		GitTreeState: "clean",
		BuildDate:    "2018-04-16T21:02:05Z",
		GoVersion:    "go1.8.3",
		Compiler:     "gc",
		Platform:     "linux/amd64",
	}
	nodeVersionInfo1 = version.Info{
		Major:        "1",
		Minor:        "0",
		GitVersion:   "v1.0.0-6+318faff6ad0609",
		GitCommit:    "318faff6ad060954387c1ff594bcbb4bb128577a",
		GitTreeState: "clean",
		// different build date
		BuildDate: "2018-04-17T07:16:01Z",
		GoVersion: "go1.8.3",
		Compiler:  "gc",
		Platform:  "linux/amd64",
	}
)

func versionInfoToString(v version.Info) string {
	out, err := v.ToBytes("json")
	if err != nil {
		panic("version info marshalling failed")
	}
	return string(out)
}

func TestVersionCommand(t *testing.T) {
	for _, tc := range []struct {
		name             string
		args             string
		virtletPods      map[string]string
		expectedCommands map[string]string
		errSubstring     string
		wrap             func([]byte) gm.Verifier
		noOutput         bool
	}{
		{
			name: "text",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
			expectedCommands: map[string]string{
				"virtlet-foo42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
				"virtlet-bar42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
			},
		},
		{
			name: "text/empty",
		},
		{
			name: "short",
			args: "--short",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
			expectedCommands: map[string]string{
				"virtlet-foo42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
				"virtlet-bar42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
			},
		},
		{
			name: "json",
			args: "-o json",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
			expectedCommands: map[string]string{
				"virtlet-foo42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
				"virtlet-bar42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
			},
			wrap: func(bs []byte) gm.Verifier { return gm.NewJSONVerifier(bs) },
		},
		{
			name: "yaml",
			args: "-o yaml",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
			expectedCommands: map[string]string{
				"virtlet-foo42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
				"virtlet-bar42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
			},
			wrap: func(bs []byte) gm.Verifier { return gm.NewYamlVerifier(bs) },
		},
		{
			name: "client",
			args: "--client",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
		},
		{
			name: "client+short",
			args: "--client --short",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
		},
		{
			name: "client+json",
			args: "--client -o json",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
			wrap: func(bs []byte) gm.Verifier { return gm.NewJSONVerifier(bs) },
		},
		{
			name: "client+yaml",
			args: "--client -o yaml",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
			wrap: func(bs []byte) gm.Verifier { return gm.NewYamlVerifier(bs) },
		},
		{
			name: "inconsistent",
			virtletPods: map[string]string{
				"kube-node-1": "virtlet-foo42",
				"kube-node-2": "virtlet-bar42",
			},
			expectedCommands: map[string]string{
				"virtlet-foo42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo),
				"virtlet-bar42/virtlet/kube-system: virtlet --version --version-format json": versionInfoToString(nodeVersionInfo1),
			},
			errSubstring: "some of the nodes have inconsistent Virtlet builds",
		},
		{
			name:         "bad",
			args:         "-o foobar",
			errSubstring: "bad version format",
			noOutput:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &fakeKubeClient{
				t:                t,
				virtletPods:      tc.virtletPods,
				expectedCommands: tc.expectedCommands,
			}
			var out bytes.Buffer
			cmd := NewVersionCommand(c, &out, &clientVersionInfo)
			if tc.args != "" {
				cmd.SetArgs(strings.Split(tc.args, " "))
			}
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			switch err := cmd.Execute(); {
			case err != nil && tc.errSubstring == "":
				t.Fatalf("virsh command returned an unexpected error: %v", err)
			case err == nil && tc.errSubstring != "":
				t.Fatalf("Didn't get expected error (substring %q), output: %q", tc.errSubstring, out.String())
			case err != nil && !strings.Contains(err.Error(), tc.errSubstring):
				t.Fatalf("Didn't get expected substring %q in the error: %v", tc.errSubstring, err)
			case !tc.noOutput:
				// ok
			case tc.wrap != nil:
				gm.Verify(t, tc.wrap(out.Bytes()))
			default:
				gm.Verify(t, out.Bytes())
			}
		})
	}
}
