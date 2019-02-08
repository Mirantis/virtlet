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
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/glog"
)

const (
	emulatorUserName     = "libvirt-qemu"
	defaultMountInfoPath = "/proc/self/mountinfo"
)

// DelimitedReader is an interface for reading a delimeter-separated
// data from files. It can be used for reading /sys and /proc
// information, for example.
type DelimitedReader interface {
	// ReadString returns next part of data up to (and including it)
	// the delimeter byte.
	ReadString(delim byte) (string, error)
	// Close closes the reader.
	Close() error
}

// FileSystem defines a filesystem interface interface
type FileSystem interface {
	// Mount mounts the specified source under the target path.
	// For bind mounts, bind must be true.
	Mount(source string, target string, fstype string, bind bool) error
	// Unmount unmounts the specified target directory. If detach
	// is true, MNT_DETACH option is used (disconnect the
	// filesystem for the new accesses even if it's busy).
	Unmount(target string, detach bool) error
	// IsPathAnNs verifies if the path is a mountpoint with nsfs filesystem type.
	IsPathAnNs(string) bool
	// ChownForEmulator makes a file or directory owned by the emulator user.
	ChownForEmulator(filePath string, recursive bool) error
	// GetDelimitedReader returns a DelimitedReader for the specified path.
	GetDelimitedReader(path string) (DelimitedReader, error)
	// WriteFile creates a new file with the specified path or truncates
	// the existing one, setting the specified permissions and writing
	// the data to it. Returns an error if any occured
	// during the operation.
	WriteFile(path string, data []byte, perm os.FileMode) error
}

type nullFileSystem struct{}

// NullFileSystem is a fs that's used for testing and does nothing
// instead of mounting/unmounting.
var NullFileSystem FileSystem = nullFileSystem{}

func (fs nullFileSystem) Mount(source string, target string, fstype string, bind bool) error {
	return nil
}

func (fs nullFileSystem) Unmount(target string, detach bool) error {
	return nil
}

func (fs nullFileSystem) IsPathAnNs(path string) bool {
	return false
}

func (fs nullFileSystem) ChownForEmulator(filePath string, recursive bool) error {
	return nil
}

func (fs nullFileSystem) GetDelimitedReader(path string) (DelimitedReader, error) {
	return nil, errors.New("not implemented")
}

func (fs nullFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return nil
}

type mountEntry struct {
	source, fsType string
}

type realFileSystem struct {
	sync.Mutex
	mountInfo       map[string]mountEntry
	gotEmulatorUser bool
	uid, gid        int
	mountInfoPath   string
}

// RealFileSystem provides access to the real filesystem.
var RealFileSystem FileSystem = &realFileSystem{}

func (fs *realFileSystem) ensureMountInfo() error {
	fs.Lock()
	defer fs.Unlock()
	if fs.mountInfo != nil {
		return nil
	}

	mountInfoPath := fs.mountInfoPath
	if mountInfoPath == "" {
		mountInfoPath = defaultMountInfoPath
	}

	reader, err := fs.GetDelimitedReader(mountInfoPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	fs.mountInfo = make(map[string]mountEntry)
LineReader:
	for {
		line, err := reader.ReadString('\n')
		switch err {
		case io.EOF:
			break LineReader
		case nil:
			// strip eol
			line = strings.Trim(line, "\n")

			// split and parse the entries acording to section 3.5 in
			// https://www.kernel.org/doc/Documentation/filesystems/proc.txt
			// TODO: whitespaces and control chars in names are encoded as
			// octal values (e.g. for "x x": "x\040x") what should be expanded
			// in both mount point source and target
			parts := strings.Split(line, " ")
			if len(parts) < 10 {
				glog.Errorf("bad mountinfo entry: %q", line)
			} else {
				fs.mountInfo[parts[4]] = mountEntry{source: parts[9], fsType: parts[8]}
			}
		default:
			return err
		}
	}
	return nil
}

func (fs *realFileSystem) getMountInfo(path string) (mountEntry, bool, error) {
	if err := fs.ensureMountInfo(); err != nil {
		return mountEntry{}, false, err
	}

	fs.Lock()
	defer fs.Unlock()
	entry, ok := fs.mountInfo[path]
	return entry, ok, nil
}

func (fs *realFileSystem) IsPathAnNs(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			glog.Errorf("Can't check if %q exists: %v", path, err)
		}
		return false
	}
	realpath, err := filepath.EvalSymlinks(path)
	if err != nil {
		glog.Errorf("Can't get the real path of %q: %v", path, err)
		return false
	}

	entry, isMountPoint, err := fs.getMountInfo(realpath)
	if err != nil {
		glog.Errorf("Can't check if %q is a namespace: error getting mount info: %v", path, err)
		return false
	}

	return isMountPoint && (entry.fsType == "nsfs" || entry.fsType == "proc")
}

func (fs *realFileSystem) getEmulatorUidGid() (int, int, error) {
	fs.Lock()
	defer fs.Unlock()
	if !fs.gotEmulatorUser {
		u, err := user.Lookup(emulatorUserName)
		if err != nil {
			return 0, 0, fmt.Errorf("can't find user %q: %v", emulatorUserName, err)
		}
		fs.uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return 0, 0, fmt.Errorf("bad uid %q for user %q: %v", u.Uid, emulatorUserName, err)
		}
		fs.gid, err = strconv.Atoi(u.Gid)
		if err != nil {
			return 0, 0, fmt.Errorf("bad gid %q for user %q: %v", u.Gid, emulatorUserName, err)
		}
	}
	return fs.uid, fs.gid, nil
}

func (fs *realFileSystem) ChownForEmulator(filePath string, recursive bool) error {
	// don't hold the mutex for the duration of chown
	uid, gid, err := fs.getEmulatorUidGid()
	if err != nil {
		return err
	}

	chown := os.Chown
	if recursive {
		chown = chownR
	}
	if err := chown(filePath, uid, gid); err != nil {
		return fmt.Errorf("can't set the owner of %q: %v", filePath, err)
	}
	return nil
}

func (fs *realFileSystem) GetDelimitedReader(path string) (DelimitedReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &delimitedReader{f, bufio.NewReader(f)}, nil
}

func (fs *realFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return ioutil.WriteFile(path, data, perm)
}

type delimitedReader struct {
	*os.File
	*bufio.Reader
}

var _ DelimitedReader = &delimitedReader{}

// chownR makes a file or directory owned by the emulator user recursively.
func chownR(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err == nil {
			err = os.Chown(name, uid, gid)
			if err != nil {
				glog.Warningf("Failed to change the owner of %q: %v", name, err)
			}
		}
		return err
	})
}
