// +build linux

/*
Copyright 2019 Mirantis

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

package fs

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Mount inplements Mount method of FileSystem interface.
func (fs *realFileSystem) Mount(source string, target string, fstype string, bind bool) error {
	if !bind {
		return syscall.Mount(source, target, fstype, uintptr(0), "")
	}

	// In case of bind mounts, we want to do it in the outer mount namespace.
	// This is used for hostPath volumes, for example.
	args := []string{"/usr/bin/nsenter", "-t", "1", "-m", "/bin/mount", "--bind", source, target}
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		return fmt.Errorf("mount %v: %v; output: %v", args, err, string(out))
	}

	return nil

}

// Unmount inplements Unmount method of FileSystem interface.
func (fs *realFileSystem) Unmount(target string, detach bool) error {
	flags := 0
	if detach {
		flags = syscall.MNT_DETACH
	}
	return syscall.Unmount(target, flags)
}
