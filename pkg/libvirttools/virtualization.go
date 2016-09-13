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
	"unsafe"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/utils"
	"encoding/xml"
	"github.com/golang/glog"
)

const (
	defaultMemory = 1024
	defaultVcpu   = 1
)

type Drive struct {
	riveName string `xml:"name,attr"`
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
	DiskType string `xml:"type,attr"`
	DiskDevice string `xml: "device,attr"`
	Drive Drive `xml:"drive"`
	Src Source `xml:"source"`
	Target Target `xml:"target"`
}

type Domain struct {
	XMLName xml.Name `xml:"domain"`
	DomType string `xml:"type,attr"`
	DiskList []Disk `xml:"devices"`
}

var volXML string = `
<disk type='file' device='disk'>
    <drive name='qemu' type='qcow2'/>
    <source file='%s'/>
    <target dev='vda' bus='virtio'/>
</disk>`

func (v *VirtualizationTool) processVolumes (mounts []*kubeapi.Mount, domXML string) (string, error) {
	copyDomXML := domXML
	if len(mounts) == 0 {
		return domXML, nil
	}

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
			fmt.Sprintf(volXML, path)
			disk:= &Disk{}
			err = xml.Unmarshal([]byte(volXML), disk)
			disk.Target.TargetDev = "vdc"
			if err != nil {
				return domXML, err
			}
			domainXML.DiskList = append(domainXML.DiskList, *disk)
			outArr, err := xml.MarshalIndent(domainXML, " ", "  ")
			if err != nil {
				return copyDomXML, err
			}
			glog.Info("Creating domain: %s", )
			domXML = string(outArr[:])
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
        <acpi/><apic/><pae/>
    </features>
    <vcpu>%d</vcpu>
    <os>
        <type>hvm</type>
        <boot dev='hd'/>
    </os>
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
	volumeStorage StorageBackend
	volumePool C.virStoragePoolPtr
	volumePoolName string
}

func NewVirtualizationTool(conn C.virConnectPtr, poolName string, storageBackendName string) (*VirtualizationTool, error){
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

	if status := C.createDomain(v.conn, cDomXML); status < 0 {
		return "", GetLastError()
	}

	return uuid, nil
}
