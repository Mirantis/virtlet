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

// Mounter defines mount/unmount interface
type Mounter interface {
	// Mount mounts the specified source under the target path.
	// For bind mounts, bind must be true.
	Mount(source string, target string, fstype string, bind bool) error
	// Unmount unmounts the specified target directory. If detach
	// is true, MNT_DETACH option is used (disconnect the
	// filesystem for the new accesses even if it's busy).
	Unmount(target string, detach bool) error
}

type nullMounter struct{}

func (m *nullMounter) Mount(source string, target string, fstype string, bind bool) error {
	return nil
}

func (m *nullMounter) Unmount(target string, detach bool) error {
	return nil
}

// NullMounter is a mounter that's used for testing and does nothing
// instead of mounting/unmounting.
var NullMounter Mounter = &nullMounter{}
