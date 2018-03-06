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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCommand(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "install-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	execContent := "foobar"
	execPath := filepath.Join(tmpDir, "topcmd")
	if err := ioutil.WriteFile(execPath, []byte(execContent), 0777); err != nil {
		t.Fatalf("ioutil.WriteFile(): %v", err)
	}

	cmd := NewInstallCmd(fakeCobraCommand(), execPath, tmpDir)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		t.Errorf("Error running install command: %v", err)
	}

	pluginBinaryPath := filepath.Join(tmpDir, ".kube/plugins/virt/virt")
	if bs, err := ioutil.ReadFile(pluginBinaryPath); err != nil {
		t.Errorf("Can't read the plugin file %q: %v", pluginBinaryPath, err)
	} else if string(bs) != execContent {
		t.Errorf("Bad content of the plugin file: %q instead of %q", bs, execContent)
	}

	pluginYamlPath := filepath.Join(tmpDir, ".kube/plugins/virt/plugin.yaml")
	if bs, err := ioutil.ReadFile(pluginYamlPath); err != nil {
		t.Errorf("Can't read plugin.yaml file %q: %v", pluginYamlPath, err)
	} else if !strings.Contains(string(bs), "Consectetur") || !strings.Contains(string(bs), "./virt") {
		t.Errorf("Bad plugin.yaml content:\n%s", bs)
	}
}
