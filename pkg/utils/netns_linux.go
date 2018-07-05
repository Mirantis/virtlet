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

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/golang/glog"
)

// mountSysfs adds new mount of sysfs on /sys to have a correct view
// in current netns on /sys/class/net
func mountSysfs() error {
	return syscall.Mount("none", "/sys", "sysfs", 0, "")
}

// unmountSysfs unmounts current fs bound to /sys
func unmountSysfs() error {
	return syscall.Unmount("/sys", syscall.MNT_DETACH)
}

// CallInNetNSWithSysfsRemounted enters particular netns, adds new sysfs
// mount on top of /sys, calls "toCall" callback in this netns removing
// temporary mount on /sys at the end.
func CallInNetNSWithSysfsRemounted(innerNS ns.NetNS, toCall func(ns.NetNS) error) error {
	return innerNS.Do(func(outerNS ns.NetNS) error {
		// switch /sys to corresponding one in netns
		// to have the correct items under /sys/class/net
		if err := mountSysfs(); err != nil {
			return err
		}
		defer func() {
			if err := unmountSysfs(); err != nil {
				glog.V(3).Infof("Warning, error during umount of /sys: %v", err)
			}
		}()

		return toCall(outerNS)
	})
}
