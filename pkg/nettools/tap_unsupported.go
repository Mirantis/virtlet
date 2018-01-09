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

package nettools

import (
	"errors"
	"os"

	"github.com/vishvananda/netlink"
)

// OpenTAP opens a tap device and returns an os.File for it
func OpenTAP(devName string) (*os.File, error) {
	return nil, errors.New("not implemented")
}

// CreateTAP sets up a tap link and brings it up
func CreateTAP(devName string, mtu int) (netlink.Link, error) {
	return nil, errors.New("not implemented")
}
