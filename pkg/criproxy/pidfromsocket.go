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
	"fmt"
	"net"
	"syscall"
	"time"
)

const (
	pidSocketDialTimeout = 10 * time.Second
)

// GetPidFromSocket attempts to connect to the specified unix domain
// socket and returns the PID of the listening process upon success.
func GetPidFromSocket(socketPath string) (int, error) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{
		Name: socketPath,
		Net:  "unix",
	})
	if err != nil {
		return 0, fmt.Errorf("error connecting to %q: %v", socketPath, err)
	}
	f, err := conn.File()
	if err != nil {
		return 0, fmt.Errorf("error getting fd for unix domain socket: %v", err)
	}
	cred, err := syscall.GetsockoptUcred(int(f.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return 0, fmt.Errorf("error getting process credentials: %v", err)
	}
	return int(cred.Pid), nil
}
