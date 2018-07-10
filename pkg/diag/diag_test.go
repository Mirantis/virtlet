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

package diag

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

type diagTester struct {
	t          *testing.T
	tmpDir     string
	ds         *Set
	s          *Server
	socketPath string
	logDir     string
}

func setupLogDir(t *testing.T, baseDir string) string {
	logDir := filepath.Join(baseDir, "logs")
	// "dummy" dir is to be ignored. This also creates the log dir
	if err := os.MkdirAll(filepath.Join(logDir, "dummy"), 0777); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	for name, contents := range map[string]string{
		"log1.txt": "log1 contents",
		"log2.txt": "log2 contents",
	} {
		if err := ioutil.WriteFile(filepath.Join(logDir, name), []byte(contents), 0777); err != nil {
			t.Fatalf("WriteFile(): %v", err)
		}
	}
	return logDir
}

func newDiagTester(t *testing.T) *diagTester {
	tmpDir, err := ioutil.TempDir("", "diag-out")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	logDir := setupLogDir(t, tmpDir)
	ds := NewDiagSet()
	ds.RegisterDiagSource("foo", NewCommandSource("txt", []string{"echo", "-n", "this is foo"}))
	ds.RegisterDiagSource("bar", NewCommandSource("log", []string{"echo", "-n", "this is bar"}))
	ds.RegisterDiagSource("simple_text", NewSimpleTextSource("txt", func() (string, error) { return "baz", nil }))
	ds.RegisterDiagSource("fail", NewSimpleTextSource("txt", func() (string, error) { return "", errors.New("oops") }))
	ds.RegisterDiagSource("logdir", NewLogDirSource(logDir))
	ds.RegisterDiagSource("stack", StackDumpSource)
	s := NewServer(ds)
	socketPath := filepath.Join(tmpDir, "diag.sock")
	readyCh := make(chan struct{})
	go func() {
		s.Serve(socketPath, readyCh)
	}()
	<-readyCh
	return &diagTester{
		t:          t,
		tmpDir:     tmpDir,
		ds:         ds,
		s:          s,
		socketPath: socketPath,
		logDir:     logDir,
	}
}

func (dt *diagTester) teardown() {
	dt.s.Stop()
	os.RemoveAll(dt.tmpDir)
}

func TestDiagServer(t *testing.T) {
	dt := newDiagTester(t)
	defer dt.teardown()
	expectedResult := Result{
		Name:  "diagnostics",
		IsDir: true,
		Children: map[string]Result{
			"foo": Result{
				Name: "foo",
				Ext:  "txt",
				Data: "this is foo",
			},
			"bar": Result{
				Name: "bar",
				Ext:  "log",
				Data: "this is bar",
			},
			"simple_text": Result{
				Name: "simple_text",
				Ext:  "txt",
				Data: "baz",
			},
			"logdir": Result{
				Name:  "logdir",
				IsDir: true,
				Children: map[string]Result{
					"log1": Result{
						Name: "log1",
						Ext:  "txt",
						Data: "log1 contents",
					},
					"log2": Result{
						Name: "log2",
						Ext:  "txt",
						Data: "log2 contents",
					},
				},
			},
			"fail": Result{
				Name:  "fail",
				Error: "oops",
			},
			// stack is not compared because it's volatile
		},
	}
	dr, err := RetrieveDiagnostics(dt.socketPath)
	if err != nil {
		t.Fatalf("RetrieveDiagnostics(): %v", err)
	}
	if dr.Children != nil {
		expectedSubstr := "TestDiagServer"
		if !strings.Contains(dr.Children["stack"].Data, expectedSubstr) {
			t.Errorf("Bad go stack: expected substring %q not found:\n%s", expectedSubstr, dr.Children["stack"].Data)
		}
		// dr.Children["stack"] is volatile and can't be
		// compared reliably, so we remove it after checking it
		// for the substring above.
		delete(dr.Children, "stack")
	}
	if !reflect.DeepEqual(expectedResult, dr) {
		t.Errorf("Bad diag result. Expected:\n%s--- got ---\n%s", spew.Sdump(expectedResult), spew.Sdump(dr))
	}
	if err := dr.Unpack(dt.tmpDir); err != nil {
		t.Fatalf("Unpack(): %v", err)
	}
	diagDir := filepath.Join(dt.tmpDir, "diagnostics")
	expectedFiles := map[string]interface{}{
		"foo.txt":         "this is foo",
		"bar.log":         "this is bar",
		"simple_text.txt": "baz",
		"logdir": map[string]interface{}{
			"log1.txt": "log1 contents",
			"log2.txt": "log2 contents",
		},
	}
	files, err := testutils.DirToMap(diagDir)
	if err != nil {
		t.Fatalf("DirToMap(): %v", err)
	}
	if !reflect.DeepEqual(expectedFiles, files) {
		t.Errorf("Bad dir structure after Unpack(). Expected:\n%s--- got ---\n%s", spew.Sdump(expectedFiles), spew.Sdump(files))
	}
}
