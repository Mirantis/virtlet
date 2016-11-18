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
	"strings"
	"unsafe"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"encoding/xml"
	"github.com/Mirantis/virtlet/pkg/bolttools"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/golang/glog"
)

const (
	defaultMemory     = 1024
	defaultMemoryUnit = "MiB"
)

type Driver struct {
	DriverName string `xml:"name,attr"`
	DriverType string `xml:"type,attr"`
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
	Driver     Driver `xml:"driver"`
	Src        Source `xml:"source"`
	Target     Target `xml:"target"`
}

type Devices struct {
	DiskList      []Disk         `xml:"disk"`
	Input         Input          `xml:"input"`
	Graphics      Graphics       `xml:"graphics"`
	Serial        Serial         `xml:"serial"`
	Console       Console        `xml:"console"`
	Sonud         Sound          `xml:"sound"`
	InterfaceList []NetInterface `xml:"interface"`
	Items         []Tag          `xml:",any"`
}

type Tag struct {
	XMLName xml.Name
	Content string `xml:",innerxml"`
}

type Memory struct {
	Memory string `xml:",chardata"`
	Unit   string `xml:"unit,attr"`
}

type Domain struct {
	XMLName xml.Name `xml:"domain"`
	DomType string   `xml:"type,attr"`
	Memory  Memory   `xml:"memory"`
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

type NetInterface struct {
	Type   string          `xml:"type,attr"`
	Model  InterfaceModel  `xml:"model"`
	Source InterfaceSource `xml:"source"`
	Target InterfaceTarget `xml:"target"`
	Mac    InterfaceMac    `xml:"mac"`
}

type InterfaceModel struct {
	Type string `xml:"type,attr"`
}

type InterfaceTarget struct {
	Device string `xml:"dev,attr"`
}

type InterfaceSource struct {
	Network string `xml:"network,attr"`
}

type InterfaceMac struct {
	Address string `xml:"address,attr"`
}

var volXML string = `
<disk type='file' device='disk'>
    <driver name='qemu' type='raw'/>
    <source file='%s'/>
    <target dev='vda' bus='virtio'/>
</disk>`

func (v *VirtualizationTool) createVolumes(containerName string, mounts []*kubeapi.Mount, domXML string) (string, error) {
	copyDomXML := domXML
	if len(mounts) == 0 {
		return domXML, nil
	}
	glog.V(2).Infof("INPUT domain:\n%s\n\n", domXML)
	domainXML := &Domain{}
	err := xml.Unmarshal([]byte(domXML), domainXML)
	if err != nil {
		return domXML, err
	}

	for _, mount := range mounts {
		volumeName := containerName + "_" + strings.Replace(mount.GetContainerPath(), "/", "_", -1)
		if mount.GetHostPath() != "" {
			vol, err := LookupVol(volumeName, v.volumePool)
			if vol == nil {
				vol, err = v.volumeStorage.CreateVol(v.volumePool, volumeName, defaultCapacity, defaultCapacityUnit)
			}
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
			break
		}
	}
	return domXML, nil
}

func generateDomXML(name string, memoryUnit string, memory int64, uuid string, cpuNum int, cpuShare int64, cpuPeriod int64, cpuQuota int64, imageFilepath, devName, hwAddress string) string {
	domXML := `
<domain type='kvm'>
    <name>%s</name>
    <uuid>%s</uuid>
    <memory unit='%s'>%d</memory>
    <vcpu>%d</vcpu>
    <cputune>
        <shares>%d</shares>
        <period>%d</period>
        <quota>%d</quota>
    </cputune>
    <os>
        <type>hvm</type>
        <boot dev='hd'/>
    </os>
    <features>
        <acpi/><apic/>
    </features>
    <on_poweroff>destroy</on_poweroff>
    <on_reboot>restart</on_reboot>
    <on_crash>restart</on_crash>
    <devices>
        <emulator>/usr/bin/kvm</emulator>
        <disk type='file' device='disk'>
            <driver name='qemu' type='qcow2'/>
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
        <interface type='network'>
            <model type='virtio' />
            <target dev='%s' />
	    <mac address='%s' />
            <source network='%s' />
        </interface>
    </devices>
</domain>`
	return fmt.Sprintf(domXML, name, uuid, memoryUnit, memory, cpuNum, cpuShare, cpuPeriod, cpuQuota, imageFilepath, devName, hwAddress, defaultNetName)
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

func (v *VirtualizationTool) CreateContainer(boltClient *bolttools.BoltClient, in *kubeapi.CreateContainerRequest, imageFilepath, devName, hwAddress string) (string, error) {
	uuid, err := utils.NewUuid()
	if err != nil {
		return "", err
	}

	config := in.GetConfig()
	name := config.GetMetadata().GetName()
	if name == "" {
		name = uuid
	} else {
		//check whether the domain with such name already exists, need to stop&destroy&undefine it then
		domain, err := v.GetDomainByName(name)
		if domain != nil {
			if domainID, err := v.GetDomainUUID(domain); err == nil {
				//TODO: This is temp workaround for returning existent domain on create container call to overcome SyncPod issues
				return domainID, nil
				//glog.V(2).Infof("Removing domain with name: %s and id: %s", name, domainID)
				//v.RemoveContainer(domainID)
			} else {
				glog.Errorf("Failed to get UUID for domain with name: %s due to %v", name, err)
			}
		} else {
			glog.Errorf("Failed to find domain with name: %s due to %v", name, err)
		}

	}

	boltClient.SetContainer(uuid, in.GetPodSandboxId(), config.GetImage().GetImage(), config.Labels, config.Annotations)

	memory := config.GetLinux().GetResources().GetMemoryLimitInBytes()
	memoryUnit := "b"
	if memory == 0 {
		memory = defaultMemory
		memoryUnit = defaultMemoryUnit
	}

	cpuNum, err := utils.GetvCPUsNum()
	if err != nil {
		return "", err
	}

	cpuShares := config.GetLinux().GetResources().GetCpuShares()
	cpuPeriod := config.GetLinux().GetResources().GetCpuPeriod()
	cpuQuota := config.GetLinux().GetResources().GetCpuQuota()

	domXML := generateDomXML(name, memoryUnit, memory, uuid, cpuNum, cpuShares, cpuPeriod, cpuQuota, imageFilepath, devName, hwAddress)
	domXML, err = v.createVolumes(name, in.Config.Mounts, domXML)
	if err != nil {
		return "", err
	}

	domXML = strings.Replace(domXML, "\"", "'", -1)
	glog.V(2).Infof("Creating domain:\n%s", domXML)
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

func (v *VirtualizationTool) GetDomainByName(name string) (C.virDomainPtr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	domain := C.virDomainLookupByName(v.conn, cName)

	if domain == nil {
		return nil, GetLastError()
	}

	return domain, nil
}

func (v *VirtualizationTool) GetDomainUUID(domain C.virDomainPtr) (string, error) {
	uuid := make([]byte, C.VIR_UUID_STRING_BUFLEN)

	if status := C.virDomainGetUUIDString(domain, (*C.char)(unsafe.Pointer(&uuid[0]))); status < 0 {
		return "", GetLastError()
	}

	return string(uuid[:len(uuid)-1]), nil
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
	return filter.State == nil || container.GetState() == filter.GetState()
}

func (v *VirtualizationTool) ListContainers(boltClient *bolttools.BoltClient, filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
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
		id, err := v.GetDomainUUID(domain)
		if err != nil {
			return nil, err
		}

		containerInfo, err := boltClient.GetContainerInfo(id)
		if err != nil {
			return nil, err
		}
		if containerInfo == nil {
			continue
		}

		podSandboxId := containerInfo.SandboxId

		if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
			return nil, GetLastError()
		}

		metadata := &kubeapi.ContainerMetadata{
			Name: &id,
		}

		image := &kubeapi.ImageSpec{Image: &containerInfo.Image}

		imageRef := containerInfo.Image

		containerState := libvirtToKubeState(domainInfo)

		createdAt := containerInfo.CreatedAt

		container := &kubeapi.Container{
			Id:           &id,
			PodSandboxId: &podSandboxId,
			Metadata:     metadata,
			Image:        image,
			ImageRef:     &imageRef,
			State:        &containerState,
			CreatedAt:    &createdAt,
			Labels:       containerInfo.Labels,
			Annotations:  containerInfo.Annotations,
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

	domain := C.virDomainLookupByUUIDString(v.conn, cContainerId)
	if domain == nil {
		return nil, GetLastError()
	}
	defer C.virDomainFree(domain)

	if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
		return nil, GetLastError()
	}

	containerState := libvirtToKubeState(domainInfo)

	return &kubeapi.ContainerStatus{
		Id:       &containerId,
		Metadata: &kubeapi.ContainerMetadata{},
		State:    &containerState,
	}, nil
}
