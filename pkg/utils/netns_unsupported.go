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

package utils

import (
	"errors"

	"github.com/containernetworking/cni/pkg/ns"
)

// CallInNetNSWithSysfsRemounted is a placeholder for unsupported systems
func CallInNetNSWithSysfsRemounted(innerNS ns.NetNS, toCall func(ns.NetNS) error) error {
	return errors.New("not implemented")
}
