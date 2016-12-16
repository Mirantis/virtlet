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
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"encoding/xml"

	"github.com/Mirantis/virtlet/pkg/bolttools"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/fields"
)

const (
	defaultMemory     = 1024
	defaultMemoryUnit = "MiB"
	defaultDomainType = "kvm"
	defaultEmulator   = "/usr/bin/kvm"
	noKvmDomainType   = "qemu"
	noKvmEmulator     = "/usr/bin/qemu-system-x86_64"
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

type CommandLine struct {
	XMLName xml.Name     `xml:"http://libvirt.org/schemas/domain/qemu/1.0 commandline"`
	Args    []string     `xml:"http://libvirt.org/schemas/domain/qemu/1.0 arg"`
	Env     []CommandEnv `xml:"http://libvirt.org/schemas/domain/qemu/1.0 env"`
}

type CommandEnv struct {
	XMLName xml.Name `xml:"http://libvirt.org/schemas/domain/qemu/1.0 env"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
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

func canUseKvm() bool {
	if os.Getenv("VIRTLET_DISABLE_KVM") != "" {
		glog.V(2).Infof("VIRTLET_DISABLE_KVM env var not empty, using plain qemu")
		return false
	}
	return true
}

func generateDomXML(useKvm bool, name string, memory int64, memoryUnit string, uuid string, cpuNum int, cpuShare int64, cpuPeriod int64, cpuQuota int64, imageFilepath, netNSPath, cniConfig string) string {
	domainType := defaultDomainType
	emulator := defaultEmulator
	if !useKvm {
		domainType = noKvmDomainType
		emulator = noKvmEmulator
	}
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(cniConfig)); err != nil {
		glog.Errorf("EscapeText() failed: %v", err)
	}
	cniConfigEscaped := buf.String()
	domXML := `
<domain type='%s'>
    <name>%s-%s</name>
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
        <emulator>/vmwrapper</emulator>
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
    </devices>
    <commandline xmlns='http://libvirt.org/schemas/domain/qemu/1.0'>
      <env name='VIRTLET_EMULATOR' value='%s'/>
      <env name='VIRTLET_NS' value='%s'/>
      <env name='VIRTLET_CNI_CONFIG' value='%s'/>
    </commandline>
</domain>`
	return fmt.Sprintf(domXML, domainType, uuid, name, uuid, memoryUnit, memory, cpuNum, cpuShare, cpuPeriod, cpuQuota, imageFilepath, emulator, netNSPath, cniConfigEscaped)
}

var volXML string = `
<disk type='file' device='disk'>
    <driver name='qemu' type='raw'/>
    <source file='%s'/>
    <target dev='vda' bus='virtio'/>
</disk>`

func (v *VirtualizationTool) createBootImageSnapshot(imageName, backingStorePath string) (string, error) {
	vol, err := v.volumeStorage.CreateSnapshot(v.volumePool, imageName, defaultCapacity, defaultCapacityUnit, backingStorePath)

	if err != nil {
		return "", err
	}
	path, err := VolGetPath(vol)

	if err != nil {
		return "", err
	}

	return path, err
}

func (v *VirtualizationTool) createVolumes(containerName string, mounts []*kubeapi.Mount, domXML string) (string, error) {
	copyDomXML := domXML
	if len(mounts) == 0 {
		return domXML, nil
	}
	glog.V(3).Infof("INPUT domain:\n%s\n\n", domXML)
	domainXML := &Domain{}
	err := xml.Unmarshal([]byte(domXML), domainXML)
	if err != nil {
		return domXML, err
	}

	for _, mount := range mounts {
		volumeName := containerName + "_" + strings.Replace(mount.GetContainerPath(), "/", "_", -1)
		if mount.GetHostPath() != "" {
			vol, err := LookupVolumeByName(volumeName, v.volumePool)
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

func (v *VirtualizationTool) CreateContainer(boltClient *bolttools.BoltClient, in *kubeapi.CreateContainerRequest, imageFilepath, netNSPath, cniConfig string) (string, error) {
	uuid, err := utils.NewUuid()
	if err != nil {
		return "", err
	}

	config := in.GetConfig()
	name := config.GetMetadata().GetName()
	sandboxId := in.GetPodSandboxId()
	if name == "" {
		name = uuid
	} else {
		// check whether the domain with such name already exists, if so - return it's uuid
		domainName := sandboxId + "-" + name
		domain, _ := v.GetDomainByName(domainName)
		if domain != nil {
			if domainID, err := v.GetDomainUUID(domain); err == nil {
				// TODO: This is temp workaround for returning existent domain on create container call to overcome SyncPod issues
				return domainID, nil
			} else {
				glog.Errorf("Failed to get UUID for domain with name: %s due to %v", domainName, err)
				return "", fmt.Errorf("Failure in communication with libvirt: %v", err)
			}
		}
	}

	snapshotImage, err := v.createBootImageSnapshot("snapshot_"+uuid, imageFilepath)
	if err != nil {
		return "", err
	}

	boltClient.SetContainer(uuid, sandboxId, config.GetImage().GetImage(), snapshotImage, config.Labels, config.Annotations)

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

	domXML := generateDomXML(canUseKvm(), name, memory, memoryUnit, uuid, cpuNum, cpuShares, cpuPeriod, cpuQuota, snapshotImage, netNSPath, cniConfig)
	domXML, err = v.createVolumes(name, in.Config.Mounts, domXML)
	if err != nil {
		return "", err
	}

	domXML = strings.Replace(domXML, "\"", "'", -1)
	glog.V(2).Infof("Creating domain:\n%s", domXML)
	cDomXML := C.CString(domXML)
	defer C.free(unsafe.Pointer(cDomXML))

	status := C.defineDomain(v.conn, cDomXML)
	if err := cErrorHandler.Convert(status); err != nil {
		return "", err
	}

	cContainerId := C.CString(uuid)
	defer C.free(unsafe.Pointer(cContainerId))
	domain := C.virDomainLookupByUUIDString(v.conn, cContainerId)
	if domain == nil {
		return "", GetLibvirtLastError()
	}
	defer C.virDomainFree(domain)
	var domainInfo C.virDomainInfo
	if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
		return "", GetLibvirtLastError()
	}

	return uuid, nil
}

func (v *VirtualizationTool) GetDomainByName(name string) (C.virDomainPtr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	domain := C.virDomainLookupByName(v.conn, cName)

	if domain == nil {
		return nil, GetLibvirtLastError()
	}

	return domain, nil
}

func (v *VirtualizationTool) GetDomainUUID(domain C.virDomainPtr) (string, error) {
	uuid := make([]byte, C.VIR_UUID_STRING_BUFLEN)

	if status := C.virDomainGetUUIDString(domain, (*C.char)(unsafe.Pointer(&uuid[0]))); status < 0 {
		return "", GetLibvirtLastError()
	}

	return string(uuid[:len(uuid)-1]), nil
}

func (v *VirtualizationTool) StartContainer(containerId string) error {
	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	status := C.createDomain(v.conn, cContainerId)
	if err := cErrorHandler.Convert(status); err != nil {
		return err
	}

	return nil
}

func (v *VirtualizationTool) StopContainer(containerId string) error {
	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	status := C.stopDomain(v.conn, cContainerId)
	if err := cErrorHandler.Convert(status); err != nil {
		return err
	}

	return nil
}

func (v *VirtualizationTool) RemoveContainer(containerId string) error {
	v.StopContainer(containerId)

	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	status := C.destroyAndUndefineDomain(v.conn, cContainerId)
	if err := cErrorHandler.Convert(status); err != nil {
		return err
	}

	return nil
}

func libvirtToKubeState(domainInfo C.virDomainInfo, lastState kubeapi.ContainerState) kubeapi.ContainerState {
	var containerState kubeapi.ContainerState

	switch domainInfo.state {
	case C.VIR_DOMAIN_RUNNING:
		containerState = kubeapi.ContainerState_RUNNING
	case C.VIR_DOMAIN_PAUSED:
		if lastState == kubeapi.ContainerState_CREATED {
			containerState = kubeapi.ContainerState_CREATED
		} else {
			containerState = kubeapi.ContainerState_EXITED
		}
	case C.VIR_DOMAIN_SHUTDOWN:
		containerState = kubeapi.ContainerState_EXITED
	case C.VIR_DOMAIN_SHUTOFF:
		if lastState == kubeapi.ContainerState_CREATED {
			containerState = kubeapi.ContainerState_CREATED
		} else {
			containerState = kubeapi.ContainerState_EXITED
		}
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
	if filter != nil {
		//TODO: Get rid of checking filter Status is nil after fix in CRI
		if filter.State != nil && container.GetState() != filter.GetState() {
			return false
		}
		filterSelector := filter.GetLabelSelector()
		if filterSelector != nil {
			sel := fields.SelectorFromSet(filterSelector)
			if !sel.Matches(fields.Set(container.GetLabels())) {
				return false
			}
		}
	}
	return true
}

func (v *VirtualizationTool) getContainer(boltClient *bolttools.BoltClient, domain *C.struct__virDomain) (*kubeapi.Container, error) {
	var domainInfo C.virDomainInfo
	containerId, err := v.GetDomainUUID(domain)
	if err != nil {
		return nil, err
	}
	containerInfo, err := boltClient.GetContainerInfo(containerId)
	if err != nil {
		return nil, err
	}
	if containerInfo == nil {
		return nil, nil
	}

	podSandboxId := containerInfo.SandboxId

	if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
		return nil, GetLibvirtLastError()
	}

	metadata := &kubeapi.ContainerMetadata{
		Name: &containerId,
	}

	image := &kubeapi.ImageSpec{Image: &containerInfo.Image}

	containerState := libvirtToKubeState(domainInfo, containerInfo.State)
	if containerInfo.State != containerState {
		if err := boltClient.UpdateState(containerId, byte(containerState)); err != nil {
			return nil, err
		}
		startedAt := time.Now().UnixNano()
		strStartedAt := strconv.FormatInt(startedAt, 10)
		if containerState == kubeapi.ContainerState_RUNNING {
			if err := boltClient.UpdateStartedAt(containerId, strStartedAt); err != nil {
				return nil, err
			}
		}
		containerInfo.StartedAt = startedAt
	}

	container := &kubeapi.Container{
		Id:           &containerId,
		PodSandboxId: &podSandboxId,
		Metadata:     metadata,
		Image:        image,
		ImageRef:     &containerInfo.Image,
		State:        &containerState,
		CreatedAt:    &containerInfo.CreatedAt,
		Labels:       containerInfo.Labels,
		Annotations:  containerInfo.Annotations,
	}
	return container, nil
}

func (v *VirtualizationTool) ListContainers(boltClient *bolttools.BoltClient, filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
	containers := make([]*kubeapi.Container, 0)

	if filter != nil {
		if filter.GetId() != "" {
			// Verify if there is container metadata
			containerInfo, err := boltClient.GetContainerInfo(filter.GetId())
			if err != nil {
				return nil, err
			}
			if containerInfo == nil {
				// There's no such container - looks like it's already removed, so return an empty list
				return containers, nil
			}

			// Query libvirt for domain found in bolt
			// TODO: Distinguish lack of domain from other errors
			domainPtr, err := v.GetDomainPointerById(filter.GetId())
			defer C.virDomainFree(domainPtr)
			if err != nil {
				// There's no such domain - looks like it's already removed, so return an empty list
				return containers, nil
			}
			container, err := v.getContainer(boltClient, domainPtr)
			if err != nil {
				return nil, err
			}

			if filter.GetPodSandboxId() != "" && container.GetPodSandboxId() != filter.GetPodSandboxId() {
				return containers, nil
			}
			if filterContainer(container, filter) {
				containers = append(containers, container)
			}
			return containers, nil
		} else if filter.GetPodSandboxId() != "" {
			domainID, err := boltClient.GetPodSandboxContainerID(filter.GetPodSandboxId())
			if err != nil {
				// There's no such sandbox - looks like it's already removed, so return an empty list
				return containers, nil
			}
			// Verify if there is container metadata
			containerInfo, err := boltClient.GetContainerInfo(domainID)
			if err != nil {
				return nil, err
			}
			if containerInfo == nil {
				// There's no such container - looks like it's already removed, but still is mentioned in sandbox
				return nil, fmt.Errorf("Container metadata not found, but it's still mentioned in sandbox %s", filter.GetPodSandboxId())
			}

			// TODO: Distinguish lack of domain from other errors
			domainPtr, err := v.GetDomainPointerById(domainID)
			defer C.virDomainFree(domainPtr)
			if err != nil {
				// There's no such domain - looks like it's already removed, so return an empty list
				return containers, nil
			}
			container, err := v.getContainer(boltClient, domainPtr)
			if err != nil {
				return nil, err
			}
			if filterContainer(container, filter) {
				containers = append(containers, container)
			}
			return containers, nil
		}
	}

	// Get list of all defined domains from libvirt and check each container against remained filter settings
	var cList *C.virDomainPtr
	count := C.virConnectListAllDomains(v.conn, (**C.virDomainPtr)(&cList), 0)
	if count < 0 {
		return nil, GetLibvirtLastError()
	}
	header := reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(cList)),
		Len:  int(count),
		Cap:  int(count),
	}
	domains := *(*[]C.virDomainPtr)(unsafe.Pointer(&header))

	for _, domainPtr := range domains {
		container, err := v.getContainer(boltClient, domainPtr)
		if err != nil {
			return nil, err
		}

		if filterContainer(container, filter) {
			containers = append(containers, container)
		}
	}

	return containers, nil
}

func (v *VirtualizationTool) GetDomainPointerById(containerId string) (*C.struct__virDomain, error) {
	cContainerId := C.CString(containerId)
	defer C.free(unsafe.Pointer(cContainerId))

	domain := C.virDomainLookupByUUIDString(v.conn, cContainerId)
	if domain == nil {
		// TODO: Distinguish lack of domain from other errors
		return nil, GetLibvirtLastError()
	}
	return domain, nil
}

func (v *VirtualizationTool) ContainerStatus(boltClient *bolttools.BoltClient, containerId string) (*kubeapi.ContainerStatus, error) {
	var domainInfo C.virDomainInfo

	domain, err := v.GetDomainPointerById(containerId)
	defer C.virDomainFree(domain)
	if err != nil {
		return nil, err
	}

	if status := C.virDomainGetInfo(domain, &domainInfo); status < 0 {
		return nil, GetLibvirtLastError()
	}

	containerInfo, err := boltClient.GetContainerInfo(containerId)
	if err != nil {
		return nil, err
	}

	if containerInfo == nil {
		return nil, fmt.Errorf("missing containerInfo for containerId: %s", containerId)
	}

	containerState := libvirtToKubeState(domainInfo, containerInfo.State)
	if containerInfo.State != containerState {
		if err := boltClient.UpdateState(containerId, byte(containerState)); err != nil {
			return nil, err
		}
		startedAt := time.Now().UnixNano()
		strStartedAt := strconv.FormatInt(startedAt, 10)
		if containerState == kubeapi.ContainerState_RUNNING {
			if err := boltClient.UpdateStartedAt(containerId, strStartedAt); err != nil {
				return nil, err
			}
		}
		containerInfo.StartedAt = startedAt
	}

	image := &kubeapi.ImageSpec{Image: &containerInfo.Image}

	return &kubeapi.ContainerStatus{
		Id:        &containerId,
		Metadata:  &kubeapi.ContainerMetadata{},
		Image:     image,
		ImageRef:  &containerInfo.Image,
		State:     &containerState,
		CreatedAt: &containerInfo.CreatedAt,
		StartedAt: &containerInfo.StartedAt,
	}, nil
}
