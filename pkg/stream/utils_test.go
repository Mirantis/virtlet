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

package stream

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

func TestGetPidFromConnection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("getPidFromConnection only works on Linux")
	}
	socket, err := ioutil.TempFile(os.TempDir(), "utilTest")
	defer os.Remove(socket.Name())
	socketPath := socket.Name()

	tc := testutils.RunProcess(t, "nc", []string{"-l", "-U", socketPath}, nil)
	defer tc.Stop()

	// wait for nc to start
	time.Sleep(2 * time.Second)

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal("Error when connecting to socket:", err)
	}

	pid, err := getPidFromConnection(conn)

	if err != nil {
		t.Errorf("Couldn't get pid from Unix socket: %v", err)
	}
	if pid != int32(tc.Pid()) {
		t.Errorf("Wrong pid from getPidFromConnection. Expected: %d, got %d", tc.Pid(), pid)
	}
}

func TestGetProcessEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("getProcessEnvironment only works on Linux")
	}

	tc := testutils.RunProcess(t, "sleep", []string{"10"}, []string{
		"FOO=1",
		"BAR=asd",
	})
	defer tc.Stop()
	waitForProcessChange(t, tc.Pid())
	env, err := getProcessEnvironment(int32(tc.Pid()))
	if err != nil {
		t.Error(err)
	}
	for k, v := range map[string]string{"FOO": "1", "BAR": "asd"} {
		envVal, ok := env[k]
		if !ok {
			t.Errorf("%s variable not found in env", k)
		}
		if envVal != v {
			t.Errorf("%s variable value not equal to %s", k, v)
		}
	}
}

func waitForProcessChange(t *testing.T, pid int) {
	ownPid := os.Getpid()
	ownCmdline, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/cmdline", ownPid))
	if err != nil {
		t.Fatalf("cannot read own cmdline: %v", err)
	}
	processCmdlineLocation := fmt.Sprintf("/proc/%d/cmdline", pid)
	for {
		processCmdline, err := ioutil.ReadFile(processCmdlineLocation)
		if err != nil {
			t.Fatalf("cannot read cmdline for pid %q: %v", pid, err)
		}
		if string(processCmdline) != string(ownCmdline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}
