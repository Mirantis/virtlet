// +build linux

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
	"syscall"
)

// MountSysfs adds new mount of sysfs on /sys to have a correct view
// in current netns on /sys/class/net
func MountSysfs() error {
	return syscall.Mount("none", "/sys", "sysfs", 0, "")
}

// UnmountSysfs unmounts current fs bound to /sys
func UnmountSysfs() error {
	return syscall.Unmount("/sys", syscall.MNT_DETACH)
}
