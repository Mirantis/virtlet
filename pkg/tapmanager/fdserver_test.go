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

package tapmanager

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

type sampleFDData struct {
	Content string
}

type sampleFDSource struct {
	tmpDir string
	files  map[string]*os.File
}

var _ FDSource = &sampleFDSource{}

func newSampleFDSource(tmpDir string) *sampleFDSource {
	return &sampleFDSource{
		tmpDir: tmpDir,
		files:  make(map[string]*os.File),
	}
}

func (s *sampleFDSource) GetFD(key string, data []byte) (int, []byte, error) {
	var fdData sampleFDData
	if err := json.Unmarshal(data, &fdData); err != nil {
		return 0, nil, fmt.Errorf("error unmarshalling json: %v", err)
	}
	if _, found := s.files[key]; found {
		return 0, nil, fmt.Errorf("file already exists: %q", key)
	}
	filename := filepath.Join(s.tmpDir, key)
	f, err := os.Create(filename)
	if err != nil {
		return 0, nil, fmt.Errorf("error creating file %q: %v", filename, err)
	}
	if err := os.Remove(f.Name()); err != nil {
		f.Close()
		return 0, nil, fmt.Errorf("Remove(): %v", err)
	}
	if _, err := f.Write([]byte(fdData.Content)); err != nil {
		f.Close()
		return 0, nil, fmt.Errorf("Write(): %v", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return 0, nil, fmt.Errorf("Seek(): %v", err)
	}
	s.files[key] = f
	return int(f.Fd()), []byte("abcdef"), nil
}

func (s *sampleFDSource) Release(key string) error {
	f, found := s.files[key]
	if !found {
		return fmt.Errorf("file not found: %q", key)
	}
	delete(s.files, key)
	if err := f.Close(); err != nil {
		return fmt.Errorf("can't close file %q: %v", f.Name(), err)
	}
	return nil
}

func (s *sampleFDSource) GetInfo(key string) ([]byte, error) {
	_, found := s.files[key]
	if !found {
		return nil, fmt.Errorf("file not found: %q", key)
	}
	return []byte("info_" + key), nil
}

func (s *sampleFDSource) isEmpty() bool {
	return len(s.files) != 0
}

func verifyFD(t *testing.T, c *FDClient, key string, data string) {
	fd, info, err := c.GetFD(key)
	if err != nil {
		t.Fatalf("GetFD(): %v", err)
	}

	expectedInfo := "info_" + key
	if string(info) != expectedInfo {
		t.Errorf("bad info: %q instead of %q", info, expectedInfo)
	}

	f1 := os.NewFile(uintptr(fd), "acquired-fd")
	defer f1.Close()

	content, err := ioutil.ReadAll(f1)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}

	if string(content) != data {
		t.Errorf("bad content: %q instead of %q", content, data)
	}
}

func TestFDServer(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "pass-fd-test")
	if err != nil {
		t.Fatalf("ioutil.TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "passfd")
	src := newSampleFDSource(tmpDir)
	s := NewFDServer(socketPath, src)
	if err := s.Serve(); err != nil {
		t.Fatalf("Serve(): %v", err)
	}
	defer s.Stop()
	c := NewFDClient(socketPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect(): %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	}()

	content := []string{"foo", "bar", "baz"}
	for _, data := range content {
		var err error
		key := "k_" + data
		respData, err := c.AddFD(key, sampleFDData{Content: data})
		if err != nil {
			t.Fatalf("AddFD(): %v", err)
		}
		expectedRespData := "abcdef"
		if string(respData) != expectedRespData {
			t.Errorf("bad data returned from add: %q instead of %q", data, expectedRespData)
		}
	}

	for _, data := range content {
		key := "k_" + data
		verifyFD(t, c, key, data)
	}

	for _, data := range content {
		key := "k_" + data
		if err := c.ReleaseFD(key); err != nil {
			t.Fatalf("ReleaseFD(): key %q: %v", key, err)
		}
	}

	// here we make sure that releasing FDs works and also that passing errors from the
	// server works, too
	expectedErrorMessage := fmt.Sprintf("server returned error: bad fd key: \"k_foo\"")
	if _, _, err := c.GetFD("k_foo"); err == nil {
		t.Errorf("GetFD didn't return an error for a released fd")
	} else if err.Error() != expectedErrorMessage {
		t.Errorf("Bad error message from GetFD: %q instead of %q", err.Error(), expectedErrorMessage)
	}

	if !src.isEmpty() {
		t.Errorf("fd source is not empty (but it should be)")
	}
}

// TODO: use separate process for the server in this test (later) -- a-la TestPassFD in https://golang.org/src/syscall/syscall_unix_test.go
// TODO: add a note about Go namespace bug & future Go version
// TODO: add a test that uses FDServer in conjunction with SetupContainerSideNetwork() and virtlet's dhcp server
// TODO: add docs
// TODO: extra task: restart dhcp / pick up netns upon virtlet restart

// LockOSThread wires the calling goroutine to its current operating system thread.
// Until the calling goroutine exits or calls UnlockOSThread, it will always
// execute in that thread, and no other goroutine can.

// The problem is spawning new threads and goroutines -- but if we lock all the threads we're good.

// https://www.weave.works/blog/linux-namespaces-and-go-don-t-mix

// exec problem -- there's chance exec will happen in wrong netns. must use nsenter
// or see if LockOSThread() is enough for syscall.Exec. But in our case (container separation)
// this will no longer be the problem, as vmwrapper will not be switching namespaces.
