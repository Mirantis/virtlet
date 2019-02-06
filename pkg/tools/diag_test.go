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
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"github.com/Mirantis/virtlet/pkg/diag"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/tests/gm"
)

var (
	fakeDiagResults = []diag.Result{
		{
			Name:  "diagnostics",
			IsDir: true,
			Children: map[string]diag.Result{
				"foo": {
					Name: "foo",
					Ext:  "txt",
					Data: "foobar",
				},
			},
		},
		{
			Name:  "diagnostics",
			IsDir: true,
			Children: map[string]diag.Result{
				"r1": {
					Name: "r1",
					Ext:  "log",
					Data: "baz1",
				},
				"r2": {
					Name: "r2",
					Ext:  "log",
					Data: "baz2",
				},
			},
		},
	}
	expectedDiagResult = diag.Result{
		Name:  "nodes",
		IsDir: true,
		Children: map[string]diag.Result{
			"kube-node-1": {
				Name:  "kube-node-1",
				IsDir: true,
				Children: map[string]diag.Result{
					"foo": fakeDiagResults[0].Children["foo"],
					"virtlet-pod-virtlet": {
						Name: "virtlet-pod-virtlet",
						Ext:  "log",
						Data: "foo-logs-virtlet",
					},
					"virtlet-pod-libvirt": {
						Name: "virtlet-pod-libvirt",
						Ext:  "log",
						Data: "foo-logs-libvirt",
					},
				},
			},
			"kube-node-2": {
				Name:  "kube-node-2",
				IsDir: true,
				Children: map[string]diag.Result{
					"r1": fakeDiagResults[1].Children["r1"],
					"r2": fakeDiagResults[1].Children["r2"],
					"virtlet-pod-virtlet": {
						Name: "virtlet-pod-virtlet",
						Ext:  "log",
						Data: "bar-logs-virtlet",
					},
					"virtlet-pod-libvirt": {
						Name: "virtlet-pod-libvirt",
						Ext:  "log",
						Data: "bar-logs-libvirt",
					},
				},
			},
		},
	}
	expectedDiagFiles = map[string]interface{}{
		"nodes": map[string]interface{}{
			"kube-node-1": map[string]interface{}{
				"foo.txt":                 "foobar",
				"virtlet-pod-virtlet.log": "foo-logs-virtlet",
				"virtlet-pod-libvirt.log": "foo-logs-libvirt",
			},
			"kube-node-2": map[string]interface{}{
				"r1.log":                  "baz1",
				"r2.log":                  "baz2",
				"virtlet-pod-virtlet.log": "bar-logs-virtlet",
				"virtlet-pod-libvirt.log": "bar-logs-libvirt",
			},
		},
	}
	fakeSonobuoyYaml = `
---
apiVersion: v1
kind: Service
metadata:
  labels:
    component: sonobuoy
    run: sonobuoy-master
  name: sonobuoy-master
  namespace: heptio-sonobuoy
spec:
  ports:
  - port: 8080
    protocol: TCP
    targetPort: 8080
  selector:
    run: sonobuoy-master
  type: ClusterIP
---
kind: ConfigMap
apiVersion: v1
metadata:
  labels:
    component: sonobuoy
  name: sonobuoy-plugins-cm
  namespace: heptio-sonobuoy
data:
  e2e.yaml: |
    sonobuoy-config:
      driver: Job
      plugin-name: e2e
      result-type: e2e
    spec:
      name: e2e
`
)

func runDiagDumpCommand(t *testing.T, input string, args ...string) []byte {
	c := &fakeKubeClient{
		t: t,
		virtletPods: map[string]string{
			"kube-node-1": "virtlet-foo42",
			"kube-node-2": "virtlet-bar42",
		},
		logs: map[string]string{
			"virtlet-foo42/virtlet/kube-system": "foo-logs-virtlet",
			"virtlet-foo42/libvirt/kube-system": "foo-logs-libvirt",
			"virtlet-bar42/virtlet/kube-system": "bar-logs-virtlet",
			"virtlet-bar42/libvirt/kube-system": "bar-logs-libvirt",
		},
		expectedCommands: map[string]string{
			"virtlet-foo42/virtlet/kube-system: virtlet --diag": string(fakeDiagResults[0].ToJSON()),
			"virtlet-bar42/virtlet/kube-system: virtlet --diag": string(fakeDiagResults[1].ToJSON()),
		},
	}
	in := bytes.NewBuffer([]byte(input))
	var out bytes.Buffer
	cmd := NewDiagCommand(c, in, &out)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("diag: %v", err)
	}
	return out.Bytes()
}

func TestDiagDumpJSON(t *testing.T) {
	data := runDiagDumpCommand(t, "", "dump", "--json")
	dr, err := diag.DecodeDiagnostics(data)
	if err != nil {
		t.Fatalf("DecodeDiagnostics: %v", err)
	}

	if !reflect.DeepEqual(expectedDiagResult, dr) {
		t.Errorf("bad diag result. Expected:\n%s\n--- got ---\n%s", expectedDiagResult.ToJSON(), dr.ToJSON())
	}
}

func verifyDiagFiles(t *testing.T, input, cmd string) {
	tmpDir, err := ioutil.TempDir("", "diag-dump-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)
	runDiagDumpCommand(t, input, cmd, tmpDir)
	files, err := testutils.DirToMap(tmpDir)
	if err != nil {
		t.Fatalf("DirToMap(): %v", err)
	}
	if !reflect.DeepEqual(expectedDiagFiles, files) {
		t.Errorf("Bad dir structure. Expected:\n%s--- got ---\n%s", spew.Sdump(expectedDiagFiles), spew.Sdump(files))
	}
}

func TestDiagDump(t *testing.T) {
	verifyDiagFiles(t, "", "dump")
}

func TestDiagUnpack(t *testing.T) {
	verifyDiagFiles(t, string(expectedDiagResult.ToJSON()), "unpack")
}

func TestDiagSonobuoy(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
	}{
		{
			name: "plain",
			args: "sonobuoy",
		},
		{
			name: "tag",
			args: "sonobuoy --tag 1.1.2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := bytes.NewBuffer([]byte(fakeSonobuoyYaml))
			var out bytes.Buffer
			cmd := NewDiagCommand(nil, in, &out)
			cmd.SetArgs(strings.Split(tc.args, " "))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			if err := cmd.Execute(); err != nil {
				t.Fatalf("diag dump: %v", err)
			}
			gm.Verify(t, gm.NewYamlVerifier(out.Bytes()))
		})
	}
}
