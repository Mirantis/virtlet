// +build !linux

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

package flexvolume

import "errors"

type LinuxMounter struct{}

var _ Mounter = &LinuxMounter{}

func NewLinuxMounter() *LinuxMounter {
	return &LinuxMounter{}
}

func (mounter *LinuxMounter) Mount(source string, target string, fstype string) error {
	return errors.New("not implemented")
}

func (mounter *LinuxMounter) Unmount(target string) error {
	return errors.New("not implemented")
}
