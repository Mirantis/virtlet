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

package criproxy

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNodeInfoArgs(t *testing.T) {
	var err error
	tmpDir, err := ioutil.TempDir("", "virtualization-test-")
	if err != nil {
		t.Fatalf("Can't create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	for _, tc := range []struct {
		in  string
		out []string
	}{
		{"", []string{""}},
		{"\x00", []string{""}},
		{"a", []string{"a"}},
		{"a\x00b", []string{"a", "b"}},
		{"a\x00b\x00", []string{"a", "b"}},
		{"abc", []string{"abc"}},
		{"abc\x00", []string{"abc"}},
		{"abc\x00\x00", []string{"abc", ""}},
		{"abc\x00def", []string{"abc", "def"}},
		{"abc\x00def\x00", []string{"abc", "def"}},
		{"abc\x00def\x00ghi", []string{"abc", "def", "ghi"}},
		{"abc\x00def\x00ghi\x00", []string{"abc", "def", "ghi"}},
		{"abc\x00def\x00ghi\x00\x00", []string{"abc", "def", "ghi", ""}},
	} {
		commandLineFile := filepath.Join(tmpDir, "cmdline")
		if err := ioutil.WriteFile(commandLineFile, []byte(tc.in), 0777); err != nil {
			t.Fatalf("ioutil.WriteFile(): %v", err)
		}
		ni, err := NodeInfoFromCommandLine(commandLineFile)
		if err != nil {
			t.Fatalf("NodeInfoFromCommandLine(): %v", err)
		}
		if !reflect.DeepEqual(tc.out, ni.KubeletArgs) {
			t.Errorf("%#v: bad args: %#v instead of %#v", tc.in, ni.KubeletArgs, tc.out)
		}
	}

}

func TestStoreLoadNodeInfo(t *testing.T) {
	var err error
	tmpDir, err := ioutil.TempDir("", "virtualization-test-")
	if err != nil {
		t.Fatalf("Can't create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	configPath := filepath.Join(tmpDir, "foobar/node.conf")
	ni := &NodeInfo{
		KubeletArgs:    []string{"abc", "def"},
		NodeName:       "kube-master",
		DockerEndpoint: "unix:///var/run/docker.sock",
	}
	if err := ni.Write(configPath); err != nil {
		t.Fatalf("Error writing config file: %v", err)
	}
	loaded, err := LoadNodeInfo(configPath)
	if err != nil {
		t.Fatalf("Error loading node info: %v", err)
	}
	if !reflect.DeepEqual(ni, loaded) {
		t.Errorf("bad node info: %#v instead of %#v", loaded, ni)
	}
}
