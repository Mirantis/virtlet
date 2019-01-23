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

package nettools

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"

	"github.com/vishvananda/netlink"
)

const (
	sizeOfIfReq = 40
	ifnamsiz    = 16
)

// Had to duplicate ifReq here as it's not exported
type ifReq struct {
	Name  [ifnamsiz]byte
	Flags uint16
	pad   [sizeOfIfReq - ifnamsiz - 2]byte
}

// OpenTAP opens a tap device and returns an os.File for it
func OpenTAP(devName string) (*os.File, error) {
	tapFile, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	var req ifReq

	// set IFF_NO_PI to not provide packet information
	// If flag IFF_NO_PI is not set each frame format is:
	// Flags [2 bytes]
	// Proto [2 bytes]
	// Raw protocol ethernet frame.
	// This extra 4-byte header breaks connectivity as in this case kernel truncates initial package
	req.Flags = uint16(syscall.IFF_TAP | syscall.IFF_NO_PI | syscall.IFF_ONE_QUEUE)
	copy(req.Name[:15], devName)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tapFile.Fd(), uintptr(syscall.TUNSETIFF), uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return nil, fmt.Errorf("tuntap IOCTL TUNSETIFF failed, errno %v", errno)
	}
	return tapFile, nil
}

// CreateTAP sets up a tap link and brings it up
func CreateTAP(devName string, mtu int) (netlink.Link, error) {
	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			Name:  devName,
			Flags: net.FlagUp,
			MTU:   mtu,
		},
		Mode: netlink.TUNTAP_MODE_TAP,
	}

	if err := netlink.LinkAdd(tap); err != nil {
		return nil, fmt.Errorf("failed to create tap interface: %v", err)
	}

	if err := netlink.LinkSetUp(tap); err != nil {
		return nil, fmt.Errorf("failed to set %q up: %v", devName, err)
	}

	// NOTE: link mtu in LinkAttrs above is actually ignored
	if err := netlink.LinkSetMTU(tap, mtu); err != nil {
		return nil, fmt.Errorf("LinkSetMTU(): %v", err)
	}

	return tap, nil
}
