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
	"io"
	"io/ioutil"
	"net"
	"path/filepath"
	"strings"
	"syscall"
)

// DetachError is special error which returned in case of container detach.
type DetachError struct{}

func (DetachError) Error() string {
	return "detached from container"
}

func getPidFromConnection(conn *net.UnixConn) (int32, error) {
	f, err := conn.File()
	if err != nil {
		return -1, err
	}
	defer f.Close()
	cred, err := syscall.GetsockoptUcred(int(f.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return -1, err
	}
	return cred.Pid, nil
}

func getProcessEnvironment(pid int32) (map[string]string, error) {
	envFile := filepath.Join("/proc", fmt.Sprint(pid), "environ")
	all, err := ioutil.ReadFile(envFile)
	if err != nil {
		return nil, err
	}
	env := map[string]string{}

	for _, v := range strings.Split(string(all), "\000") {
		s := strings.SplitN(v, "=", 2)
		key := s[0]
		value := ""
		if len(s) == 2 {
			value = s[1]
		}
		env[key] = value
	}
	return env, nil
}

// based on https://github.com/kubernetes-incubator/cri-o/blob/master/utils/utils.go#L90
// CopyDetachable is similar to io.Copy but support a detach key sequence to break out.
func CopyDetachable(dst io.Writer, src io.Reader, keys []byte) (written int64, err error) {
	if len(keys) == 0 {
		// Default key : ^]
		keys = []byte{29}
	}

	buf := make([]byte, 32*1024)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			preservBuf := []byte{}
			for i, key := range keys {
				preservBuf = append(preservBuf, buf[0:nr]...)
				if nr != 1 || buf[0] != key {
					break
				}
				if i == len(keys)-1 {
					return 0, nil
				}
				nr, er = src.Read(buf)
			}
			var nw int
			var ew error
			if len(preservBuf) > 0 {
				nw, ew = dst.Write(preservBuf)
				nr = len(preservBuf)
			} else {
				nw, ew = dst.Write(buf[0:nr])
			}
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}
