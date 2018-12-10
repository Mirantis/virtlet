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

package utils

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
)

type mountEntry struct {
	Source string
	Fs     string
}

// MountPointChecker is used to check if a directory entry is a mount point.
// It returns its source info and filesystem type.
type MountPointChecker interface {
	// CheckMountPointInfo checks if entry is a mountpoint (second returned value will be true)
	// and if so returns mountInfo for it. In other case it returns false as a second value.
	CheckMountPointInfo(string) (mountEntry, bool)
	// IsPathAnNs verifies if the path is a mountpoint with nsfs filesystem type
	IsPathAnNs(string) bool
}

type mountPointChecker struct {
	mountInfo map[string]mountEntry
}

var _ MountPointChecker = mountPointChecker{}

// NewMountPointChecker returns a new instance of MountPointChecker
func NewMountPointChecker() (MountPointChecker, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return mountPointChecker{}, err
	}
	defer file.Close()

	mi := make(map[string]mountEntry)

	reader := bufio.NewReader(file)
LineReader:
	for {
		line, err := reader.ReadString('\n')
		switch err {
		case io.EOF:
			break LineReader
		case nil:
			// strip eol
			line = strings.Trim(line, "\n")

			// split and parse entries acording to section 3.5 in
			// https://www.kernel.org/doc/Documentation/filesystems/proc.txt
			// TODO: whitespaces and control chars in names are encoded as
			// octal values (e.g. for "x x": "x\040x") what should be expanded
			// in both mount point source and target
			parts := strings.Split(line, " ")
			mi[parts[4]] = mountEntry{Source: parts[9], Fs: parts[8]}
		default:
			return mountPointChecker{}, err
		}
	}
	return mountPointChecker{mountInfo: mi}, nil
}

// CheckMountPointInfo implements CheckMountPointInfo method of MountPointChecker interface
func (mpc mountPointChecker) CheckMountPointInfo(path string) (mountEntry, bool) {
	entry, ok := mpc.mountInfo[path]
	return entry, ok
}

// IsPathNs implements IsPathAnNs method of MountPointChecker interface
func (mpc mountPointChecker) IsPathAnNs(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			glog.Errorf("Cannot verify existence of %q: %v", path, err)
		}
		return false
	}
	realpath, err := filepath.EvalSymlinks(path)
	if err != nil {
		glog.Errorf("Cannot verify real path of %q: %v", path, err)
		return false
	}

	entry, isMountPoint := mpc.CheckMountPointInfo(realpath)
	if !isMountPoint {
		return false
	}
	return entry.Fs == "nsfs" || entry.Fs == "proc"
}

type fakeMountPointChecker struct{}

// FakeMountPointChecker is defined there for unittests
var FakeMountPointChecker MountPointChecker = fakeMountPointChecker{}

// CheckMountPointInfo is a fake implementation for MountPointChecker interface
func (mpc fakeMountPointChecker) CheckMountPointInfo(path string) (mountEntry, bool) {
	return mountEntry{}, false
}

// IsPathAnNs is a fake implementation for MountPointChecker interface
func (mpc fakeMountPointChecker) IsPathAnNs(path string) bool {
	return false
}
