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
#include "virtualization.h"
*/
import "C"

import (
	"fmt"
	"reflect"
	"unsafe"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	defaultMemory = 1024
	defaultVcpu   = 1
)

func generateDomXML(name string, memory int64, uuid string, vcpu int64, imageFilepath string) string {
	domXML := `
<domain type='kvm'>
    <name>%s</name>
    <memory>%d</memory>
    <uuid>%s</uuid>
    <features>
        <acpi/><apic/>
    </features>
    <vcpu>%d</vcpu>
    <os>
        <type>hvm</type>
        <boot dev='hd'/>
    </os>
    <on_poweroff>destroy</on_poweroff>
    <on_reboot>restart</on_reboot>
    <on_crash>restart</on_crash>
    <devices>
        <emulator>/usr/libexec/qemu-kvm</emulator>
        <disk type='file' device='disk'>
            <drive name='qemu' type='qcow2'/>
            <source file='%s'/>
            <target dev='vda' bus='virtio'/>
        </disk>
        <input type='tablet' bus='usb'/>
        <graphics type='vnc' port='-1'/>
        <console type='pty'/>
        <sound model='ac97'/>
        <video>
            <model type='cirrus'/>
        </video>
    </devices>
</domain>`
	return fmt.Sprintf(domXML, name, memory, uuid, vcpu, imageFilepath)
}

type VirtualizationTool struct {
	conn C.virConnectPtr
}

func NewVirtualizationTool(conn C.virConnectPtr) *VirtualizationTool {
	return &VirtualizationTool{conn: conn}
}

func (v *VirtualizationTool) CreateContainer(in *kubeapi.CreateContainerRequest, imageFilepath string) (string, error) {
	var name string
	var memory int64
	var vcpu int64

	uuid, err := utils.NewUuid()
	if err != nil {
		return "", err
	}

	if in.Config.Metadata != nil && in.Config.Metadata.Name != nil {
		name = *in.Config.Metadata.Name
	} else {
		name = uuid
	}

	if in.Config.Linux != nil && in.Config.Linux.Resources != nil && in.Config.Linux.Resources.MemoryLimitInBytes != nil {
		memory = *in.Config.Linux.Resources.MemoryLimitInBytes
	} else {
		memory = defaultMemory
	}

	if in.Config.Linux != nil && in.Config.Linux.Resources != nil && in.Config.Linux.Resources.CpuPeriod != nil {
		vcpu = *in.Config.Linux.Resources.CpuPeriod
	} else {
		vcpu = defaultVcpu
	}

	domXML := generateDomXML(name, memory, uuid, vcpu, imageFilepath)

	cDomXML := C.CString(domXML)
	defer C.free(unsafe.Pointer(cDomXML))

	if status := C.defineAndCreateDomain(v.conn, cDomXML); status < 0 {
		return "", GetLastError()
	}

	return uuid, nil
}

func (v *VirtualizationTool) StartContainer(containerId string) error {
	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	if status := C.createDomain(v.conn, cContainerId); status < 0 {
		return GetLastError()
	}

	return nil
}

func (v *VirtualizationTool) StopContainer(containerId string) error {
	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	if status := C.stopDomain(v.conn, cContainerId); status < 0 {
		return GetLastError()
	}

	return nil
}

func (v *VirtualizationTool) RemoveContainer(containerId string) error {
	v.StopContainer(containerId)

	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	if status := C.destroyAndUndefineDomain(v.conn, cContainerId); status < 0 {
		return GetLastError()
	}

	return nil
}

func (v *VirtualizationTool) ListContainers() ([]*kubeapi.Container, error) {
	var cList *C.virDomainPtr
	count := C.virConnectListAllDomains(v.conn, (**C.virDomainPtr)(&cList), 0)
	if count < 0 {
		return nil, GetLastError()
	}
	header := reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(cList)),
		Len:  int(count),
		Cap:  int(count),
	}
	domains := *(*[]C.virDomainPtr)(unsafe.Pointer(&header))

	containers := make([]*kubeapi.Container, 0, count)

	for _, domain := range domains {
		id := C.GoString(C.virDomainGetName(domain))

		containers = append(containers, &kubeapi.Container{
			Id: &id,
		})
	}

	return containers, nil
}

func (v *VirtualizationTool) ContainerStatus(containerId string) (*kubeapi.ContainerStatus, error) {
	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	domain := C.virDomainLookupByName(v.conn, cContainerId)
	defer C.virDomainFree(domain)

	id := C.GoString(C.virDomainGetName(domain))

	return &kubeapi.ContainerStatus{
		Id: &id,
	}, nil
}
