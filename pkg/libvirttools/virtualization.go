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

	"encoding/xml"
	"github.com/Mirantis/virtlet/pkg/etcdtools"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/golang/glog"
)

const (
	defaultMemory = 1024
	defaultVcpu   = 1
)

type Drive struct {
	DriveName string `xml:"name,attr"`
	DriveType string `xml:"type,attr"`
}

type Source struct {
	SrcFile string `xml:"file,attr"`
}

type Target struct {
	TargetDev string `xml:"dev,attr"`
	TargetBus string `xml:"bus,attr"`
}

type Disk struct {
	DiskType   string `xml:"type,attr"`
	DiskDevice string `xml:"device,attr"`
	Drive      Drive  `xml:"drive"`
	Src        Source `xml:"source"`
	Target     Target `xml:"target"`
}

type Devices struct {
	DiskList []Disk   `xml:"disk"`
	Inpt     Input    `xml:"input"`
	Graph    Graphics `xml:"graphics"`
	Serial   Serial   `xml:"serial"`
	Consl    Console  `xml:"console"`
	Snd      Sound    `xml:"sound"`
	Items    []Tag    `xml:",any"`
}

type Tag struct {
	XMLName xml.Name
	Content string `xml:",innerxml"`
}

type Domain struct {
	XMLName xml.Name `xml:"domain"`
	DomType string   `xml:"type,attr"`
	Devs    Devices  `xml:"devices"`
	Items   []Tag    `xml:",any"`
}

type Input struct {
	Type string `xml:"type,attr"`
	Bus  string `xml:"bus,attr"`
}

type Graphics struct {
	Type string `xml:"type,attr"`
	Port string `xml:"port,attr"`
}

type Console struct {
	Type   string        `xml:"type,attr"`
	Target TargetConsole `xml:"target"`
}

type TargetConsole struct {
	Type string `xml:"type,attr"`
	Port string `xml:"port,attr"`
}

type Serial struct {
	Type   string       `xml:"type,attr"`
	Target TargetSerial `xml:"target"`
}

type TargetSerial struct {
	Port string `xml:"port,attr"`
}

type Sound struct {
	Model string `xml:"model,attr"`
}

var volXML string = `
<disk type='file' device='disk'>
    <drive name='qemu' type='raw'/>
    <source file='%s'/>
    <target dev='vda' bus='virtio'/>
</disk>`

func (v *VirtualizationTool) processVolumes(mounts []*kubeapi.Mount, domXML string) (string, error) {
	copyDomXML := domXML
	if len(mounts) == 0 {
		return domXML, nil
	}
	glog.Infof("INPUT domain:\n%s\n\n", domXML)
	domainXML := &Domain{}
	err := xml.Unmarshal([]byte(domXML), domainXML)
	if err != nil {
		return domXML, err
	}

	for _, mount := range mounts {
		if mount.HostPath != nil {
			vol, err := v.volumeStorage.CreateVol(v.volumePool, *mount.Name, defaultCapacity, defaultCapacityUnit)
			if err != nil {
				return domXML, err
			}
			path, err := VolGetPath(vol)
			if err != nil {
				return copyDomXML, err
			}
			err = utils.FormatDisk(path)
			if err != nil {
				return copyDomXML, err
			}
			volXML = fmt.Sprintf(volXML, path)
			disk := &Disk{}
			err = xml.Unmarshal([]byte(volXML), disk)
			disk.Target.TargetDev = "vdc"
			if err != nil {
				return domXML, err
			}
			domainXML.Devs.DiskList = append(domainXML.Devs.DiskList, *disk)
			outArr, err := xml.MarshalIndent(domainXML, " ", "  ")
			if err != nil {
				return copyDomXML, err
			}
			domXML = string(outArr[:])
			glog.Infof("Creating domain:\n%s", domXML)
			break
		}
	}
	return domXML, nil
}

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
        <emulator>/usr/bin/kvm</emulator>
        <disk type='file' device='disk'>
            <drive name='qemu' type='qcow2'/>
            <source file='%s'/>
            <target dev='vda' bus='virtio'/>
        </disk>
        <input type='tablet' bus='usb'/>
        <graphics type='vnc' port='-1'/>
        <serial type='pty'>
            <target port='0'/>
        </serial>
        <console type='pty'>
            <target type='serial' port='0'/>
        </console>
        <sound model='ac97'/>
        <video>
            <model type='cirrus'/>
        </video>
    </devices>
</domain>`
	return fmt.Sprintf(domXML, name, memory, uuid, vcpu, imageFilepath)
}

type VirtualizationTool struct {
	conn           C.virConnectPtr
	volumeStorage  StorageBackend
	volumePool     C.virStoragePoolPtr
	volumePoolName string
}

func NewVirtualizationTool(conn C.virConnectPtr, poolName string, storageBackendName string) (*VirtualizationTool, error) {
	pool, err := LookupStoragePool(conn, poolName)
	if err != nil {
		return nil, err
	}

	storageBackend, err := GetStorageBackend(storageBackendName)
	if err != nil {
		return nil, err
	}
	return &VirtualizationTool{conn: conn, volumeStorage: storageBackend, volumePool: pool, volumePoolName: poolName}, nil
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
	domXML, err = v.processVolumes(in.Config.Mounts, domXML)
	if err != nil {
		return "", err
	}

	cDomXML := C.CString(domXML)
	defer C.free(unsafe.Pointer(cDomXML))

	if status := C.defineDomain(v.conn, cDomXML); status < 0 {
		return "", GetLastError()
	}

	cContainerId := C.CString(uuid)
	defer C.free(unsafe.Pointer(cContainerId))
	domain := C.virDomainLookupByUUIDString(v.conn, cContainerId)
	if domain == nil {
		return "", GetLastError()
	}
	defer C.virDomainFree(domain)
	var domainInfo C.virDomainInfo
	if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
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

func libvirtToKubeState(domainInfo C.virDomainInfo) kubeapi.ContainerState {
	var containerState kubeapi.ContainerState

	switch domainInfo.state {
	case C.VIR_DOMAIN_RUNNING:
		containerState = kubeapi.ContainerState_RUNNING
	case C.VIR_DOMAIN_PAUSED:
		containerState = kubeapi.ContainerState_EXITED
	case C.VIR_DOMAIN_SHUTDOWN:
		containerState = kubeapi.ContainerState_EXITED
	case C.VIR_DOMAIN_SHUTOFF:
		containerState = kubeapi.ContainerState_CREATED
	case C.VIR_DOMAIN_CRASHED:
		containerState = kubeapi.ContainerState_EXITED
	case C.VIR_DOMAIN_PMSUSPENDED:
		containerState = kubeapi.ContainerState_EXITED
	default:
		containerState = kubeapi.ContainerState_UNKNOWN
	}

	return containerState
}

func filterContainer(container *kubeapi.Container, filter *kubeapi.ContainerFilter) bool {
	if filter.State != nil && *container.State != *filter.State {
		return false
	}
	return true
}

func (v *VirtualizationTool) ListContainers(etcdTool *etcdtools.VirtualizationTool, filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
	var domainInfo C.virDomainInfo
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

		if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
			return nil, GetLastError()
		}

		containerState := libvirtToKubeState(domainInfo)

		metadata := &kubeapi.ContainerMetadata{
			Name: &id,
		}

		labels, err := etcdTool.GetLabels(id)
		if err != nil {
			return nil, err
		}
		annotations, err := etcdTool.GetAnnotations(id)
		if err != nil {
			return nil, err
		}

		container := &kubeapi.Container{
			Id:          &id,
			State:       &containerState,
			Metadata:    metadata,
			Labels:      labels,
			Annotations: annotations,
		}

		if filterContainer(container, filter) {
			containers = append(containers, container)
		}
	}

	return containers, nil
}

func (v *VirtualizationTool) ContainerStatus(containerId string) (*kubeapi.ContainerStatus, error) {
	var domainInfo C.virDomainInfo

	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	domain := C.virDomainLookupByName(v.conn, cContainerId)
	if domain == nil {
		return nil, GetLastError()
	}
	defer C.virDomainFree(domain)

	id := C.GoString(C.virDomainGetName(domain))

	if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
		return nil, GetLastError()
	}

	containerState := libvirtToKubeState(domainInfo)

	return &kubeapi.ContainerStatus{
		Id:       &id,
		Metadata: &kubeapi.ContainerMetadata{},
		State:    &containerState,
	}, nil
}
