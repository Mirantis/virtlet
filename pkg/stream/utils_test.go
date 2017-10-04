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
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestGetPidFromConnection(t *testing.T) {
	socket, err := ioutil.TempFile(os.TempDir(), "utilTest")
	defer os.Remove(socket.Name())
	socketPath := socket.Name()

	cmd := exec.Command("nc", "-l", "-U", socketPath)
	if err := cmd.Start(); err != nil {
		t.Errorf("Error when starting command: %v", err)
	}
	defer cmd.Process.Kill()
	//wait for nc to start
	time.Sleep(2 * time.Second)

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{socketPath, "unix"})
	if err != nil {
		t.Fatal("Error when connecting to socket:", err)
	}

	pid, err := getPidFromConnection(conn)

	if err != nil {
		t.Errorf("Couldn't get pid from Unix socket: %v", err)
	}
	if pid != int32(cmd.Process.Pid) {
		t.Errorf("Wrong pid from getPidFromConnection. Expected: %d, got %d", cmd.Process.Pid, pid)
	}
}

func TestGetProcessEnvironment(t *testing.T) {
	cmd := exec.Command("sleep", "10")
	cmd.Env = append(os.Environ(),
		"FOO=1",
		"BAR=asd",
	)
	if err := cmd.Start(); err != nil {
		t.Errorf("Error when starting command: %v", err)
	}
	defer cmd.Process.Kill()
	env, err := getProcessEnvironment(int32(cmd.Process.Pid))
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
