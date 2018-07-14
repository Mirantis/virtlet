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

package libvirttools

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	emulatorUserName = "libvirt-qemu"
)

var emulatorUser struct {
	sync.Mutex
	initialized bool
	uid, gid    int
}

// ChownForEmulator makes a file or directory owned by the emulator user.
func ChownForEmulator(filePath string, recursive bool) error {
	emulatorUser.Lock()
	defer emulatorUser.Unlock()
	if !emulatorUser.initialized {
		u, err := user.Lookup(emulatorUserName)
		if err != nil {
			return fmt.Errorf("can't find user %q: %v", emulatorUserName, err)
		}
		emulatorUser.uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return fmt.Errorf("bad uid %q for user %q: %v", u.Uid, emulatorUserName, err)
		}
		emulatorUser.gid, err = strconv.Atoi(u.Gid)
		if err != nil {
			return fmt.Errorf("bad gid %q for user %q: %v", u.Gid, emulatorUserName, err)
		}
	}

	chown := os.Chown
	if recursive {
		chown = ChownR
	}
	if err := chown(filePath, emulatorUser.uid, emulatorUser.gid); err != nil {
		return fmt.Errorf("can't set the owner of tapmanager socket: %v", err)
	}
	return nil
}

// ChownR makes a file or directory owned by the emulator user recursively.
func ChownR(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err == nil {
			err = os.Chown(name, uid, gid)
		}
		return err
	})
}
