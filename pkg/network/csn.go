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

package network

import (
	"net"
	"os"

	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
)

// InterfaceType presents type of network interface instance
type InterfaceType int

const (
	InterfaceTypeTap InterfaceType = iota
	InterfaceTypeVF
)

type InterfaceDescription struct {
	// Type contains interface type designator
	Type InterfaceType
	// Fo contains open File object pointing to tap device inside network
	// namespace or to control file in sysfs for sr-iov VF.
	// It may be nil if the interface was recovered after restarting Virtlet.
	// It's only needed during the initial VM startup
	Fo *os.File
	// Name containes original interface name for sr-iov interface
	Name string
	// HardwareAddr contains original hardware address for CNI-created
	// veth link
	HardwareAddr net.HardwareAddr
	// PCIAddress contains a pci address for sr-iov vf interface
	PCIAddress string
	// MTU contains max transfer unit value for interface
	MTU uint16
}

// ContainerSideNetwork struct describes the container (VM) network
// namespace properties
type ContainerSideNetwork struct {
	// Result contains CNI result object describing the network settings
	Result *cnicurrent.Result
	// NsPath specifies the path to the container network namespace
	NsPath string
	// Interfaces contains a list of interfaces with data needed
	// to configure them
	Interfaces []*InterfaceDescription
}
