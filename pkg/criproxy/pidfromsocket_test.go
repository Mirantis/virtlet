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
	"net"
	"os"
	"testing"

	"github.com/Mirantis/virtlet/pkg/utils"
)

func TestGetPidFromSocket(t *testing.T) {
	socketPath, err := utils.Tempfile()
	if err != nil {
		t.Fatalf("can't get tmp socket path: %v", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen(): %v", err)
	}
	defer ln.Close()
	go func() {
		var conns []net.Conn
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}
			conns = append(conns, conn)
		}
		for _, c := range conns {
			c.Close()
		}
	}()
	pid, err := GetPidFromSocket(socketPath)
	if err != nil {
		t.Fatalf("GetPidFromSocket(): %v", err)
	}
	expectedPid := os.Getpid()
	if pid != expectedPid {
		t.Errorf("Bad pid returned: %d instead of %d", pid, expectedPid)
	}
}
