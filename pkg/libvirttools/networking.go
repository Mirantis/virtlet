/*
Copyright 2016 Mirantis

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

package libvirttools

/*
#include <libvirt/libvirt.h>
#include <libvirt/virterror.h>
#include <stdlib.h>
#include "networking.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	defaultName   = "virtlet"
	defaultBridge = "virtbr0"
)

func generateNetworkXML() string {
	xml := `
<network>
    <name>%s</name>
    <bridge name="%s" />
    <forward mode="bridge" />
</network>`
	return fmt.Sprintf(xml, defaultName, defaultBridge)
}

type NetworkingTool struct {
	conn C.virConnectPtr
}

func NewNetworkingTool(conn C.virConnectPtr) *NetworkingTool {
	return &NetworkingTool{conn: conn}
}

func (n *NetworkingTool) EnsureVirtletNetwork() error {
	cNetName := C.CString(defaultName)
	defer C.free(unsafe.Pointer(cNetName))

	if status := C.hasNetwork(n.conn, cNetName); status < 0 {
		XML := generateNetworkXML()
		cXML := C.CString(XML)
		defer C.free(unsafe.Pointer(cXML))

		if status := C.createNetwork(n.conn, cXML); status < 0 {
			return GetLastError()
		}
	}

	return nil
}
