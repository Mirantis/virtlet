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
	"net"
	"unsafe"
)

const (
	defaultName   = "virtlet"
	defaultBridge = "virtlet0"
	defaultDevice = "eth0"
)

func dottedMask(mask net.IPMask) string {
	return fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
}

func nextIP(ip net.IP) {
	for bytePos := len(ip) - 1; bytePos > 0; bytePos-- {
		ip[bytePos] += 1
		if ip[bytePos] != 0 {
			break
		}
	}
}

// addressesInSubnet returns full list of ip addresses in given subnet without net/broadcast ones
func addressesInSubnet(net net.IPNet) []string {
	var addresses []string
	for ip := net.IP.Mask(net.Mask); net.Contains(ip); nextIP(ip) {
		addresses = append(addresses, ip.String())
	}
	return addresses[1 : len(addresses)-1]
}

type networkingData struct {
	address    string
	rangeStart string
	rangeEnd   string
	netmask    string
}

func getAddressingFromSubnet(subnet string) (networkingData, error) {
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return networkingData{}, err
	}

	ones, _ := ipnet.Mask.Size()
	if ones < 3 {
		return networkingData{}, fmt.Errorf("too tiny subnet '/%d' - expected greater than /3")
	}

	addresses := addressesInSubnet(*ipnet)
	return networkingData{
		address:    addresses[0],
		rangeStart: addresses[1],
		rangeEnd:   addresses[len(addresses)-1],
		netmask:    dottedMask(ipnet.Mask),
	}, nil
}

func generateNetworkXML(subnet string, iface string) (string, error) {
	nd, err := getAddressingFromSubnet(subnet)
	if err != nil {
		return "", err
	}

	xml := `
<network>
    <name>%s</name>
    <bridge name="%s"/>
    <forward mode="route" dev="%s"/>
    <ip address="%s" netmask="%s">
        <dhcp>
	    <range start="%s" end="%s"/>
	</dhcp>
    </ip>
</network>`

	return fmt.Sprintf(
		xml, defaultName, defaultName, defaultBridge, iface,
		nd.address, nd.netmask, nd.rangeStart, nd.rangeEnd), nil
}

type NetworkingTool struct {
	conn C.virConnectPtr
}

func NewNetworkingTool(conn C.virConnectPtr) *NetworkingTool {
	return &NetworkingTool{conn: conn}
}

func (n *NetworkingTool) EnsureVirtletNetwork(subnet string, device string) error {
	cNetName := C.CString(defaultName)
	defer C.free(unsafe.Pointer(cNetName))

	if status := C.hasNetwork(n.conn, cNetName); status < 0 {
		XML, err := generateNetworkXML(subnet, device)
		if err != nil {
			return err
		}
		cXML := C.CString(XML)
		defer C.free(unsafe.Pointer(cXML))

		if status := C.createNetwork(n.conn, cXML); status < 0 {
			return GetLastError()
		}
	}

	return nil
}

func (n *NetworkingTool) PodIP(id string) (string, error) {
	cId := C.CString(id)
	defer C.free(unsafe.Pointer(cId))

	var ipPointer *C.char = nil
	defer C.free(unsafe.Pointer(ipPointer))

	if status := C.getDomIfAddr(n.conn, cId, &ipPointer); status < 0 {
		return "", GetLastError()
	}
	if ipPointer != nil {
		return C.GoString(ipPointer), nil
	}
	// TODO: get rid of this fake data
	return "10.0.0.2", nil
}
