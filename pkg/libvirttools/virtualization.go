/*
Copyright 2016-2017 Mirantis

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

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
	"k8s.io/kubernetes/pkg/fields"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	defaultMemory     = 1024
	defaultMemoryUnit = "MiB"
	defaultDomainType = "kvm"
	defaultEmulator   = "/usr/bin/kvm"
	noKvmDomainType   = "qemu"
	noKvmEmulator     = "/usr/bin/qemu-system-x86_64"

	VirtletVolumesAnnotationKeyName = "VirtletVolumes"
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

var volXMLTemplate string = `
<disk type='file' device='disk'>
    <driver name='qemu' type='raw'/>
    <source file='%s'/>
    <target dev='%s' bus='virtio'/>
</disk>`

func (v *VirtualizationTool) createBootImageSnapshot(imageName, backingStorePath string) (string, error) {
	vol, err := v.volumeStorage.CreateSnapshot(imageName, defaultCapacity, defaultCapacityUnit, backingStorePath)

	if err != nil {
		return "", err
	}

	return vol.GetPath()
}

func (v *VirtualizationTool) addAttachedVolumesXML(uuid string, virtletVolsDesc string, domXML string) (string, error) {
	copyDomXML := domXML
	glog.V(3).Infof("INPUT domain:\n%s\n\n", domXML)
	domainXML := &Domain{}
	err := xml.Unmarshal([]byte(domXML), domainXML)
	if err != nil {
		return domXML, err
	}

	volumesXML, err := v.volumeStorage.CreateVolumesToBeAttached(virtletVolsDesc, uuid)
	if err != nil {
		return domXML, err
	}

	for _, volXML := range volumesXML {
		disk := &Disk{}
		err = xml.Unmarshal([]byte(volXML), disk)
		if err != nil {
			return domXML, err
		}

		domainXML.Devs.DiskList = append(domainXML.Devs.DiskList, *disk)
		outArr, err := xml.MarshalIndent(domainXML, " ", "  ")
		if err != nil {
			return copyDomXML, err
		}

		domXML = string(outArr[:])
	}
	return domXML, nil
}

type VirtualizationTool struct {
	tool           DomainOperations
	volumeStorage  *StorageTool
	volumePoolName string
}

func NewVirtualizationTool(conn *libvirt.Connect, poolName string) (*VirtualizationTool, error) {
	storageTool, err := NewStorageTool(conn, poolName)
	if err != nil {
		return nil, err
	}
	tool := NewLibvirtDomainOperations(conn)
	return &VirtualizationTool{tool: tool, volumeStorage: storageTool}, nil
}

func (v *VirtualizationTool) CreateContainer(metadataStore metadata.MetadataStore, in *kubeapi.CreateContainerRequest, imageFilepath, netNSPath, cniConfig string) (string, error) {
	uuid, err := utils.NewUuid()
	if err != nil {
		return "", err
	}

	config := in.GetConfig()
	name := config.GetMetadata().GetName()
	sandboxId := in.GetPodSandboxId()
	sandBoxAnnotations, err := metadataStore.GetPodSandboxAnnotations(sandboxId)
	if err != nil {
		return "", err
	}

	if name == "" {
		name = uuid
	} else {
		// check whether the domain with such name already exists, if so - return it's uuid
		domainName := sandboxId + "-" + name
		domain, _ := v.tool.LookupByName(domainName)
		if domain != nil {
			if domainID, err := v.tool.GetUUIDString(domain); err == nil {
				// TODO: This is temp workaround for returning existent domain on create container call to overcome SyncPod issues
				return domainID, nil
			} else {
				glog.Errorf("Failed to get UUID for domain with name: %s due to %v", domainName, err)
				return "", fmt.Errorf("Failure in communication with libvirt: %v", err)
			}
		}
	}

	snapshotName := "snapshot_" + uuid
	snapshotImage, err := v.createBootImageSnapshot(snapshotName, imageFilepath)
	if err != nil {
		return "", err
	}

	metadataStore.SetContainer(uuid, sandboxId, config.GetImage().GetImage(), snapshotName, config.Labels, config.Annotations)

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

	if virtletVolsDesc, exists := sandBoxAnnotations[VirtletVolumesAnnotationKeyName]; exists {
		domXML, err = v.addAttachedVolumesXML(uuid, virtletVolsDesc, domXML)
		if err != nil {
			return "", err
		}
	}

	domXML = strings.Replace(domXML, "\"", "'", -1)
	glog.V(2).Infof("Creating domain:\n%s", domXML)
	if _, err := v.tool.DefineFromXML(domXML); err != nil {
		return "", err
	}

	domain, err := v.tool.LookupByUUIDString(uuid)
	if err != nil {
		return "", err
	}

	if _, err := v.tool.GetDomainInfo(domain); err != nil {
		return "", err
	}

	return uuid, nil
}

func (v *VirtualizationTool) StartContainer(containerId string) error {
	domain, err := v.tool.LookupByUUIDString(containerId)
	if err != nil {
		return err
	}

	return v.tool.Create(domain)
}

func (v *VirtualizationTool) StopContainer(containerId string) error {
	domain, err := v.tool.LookupByUUIDString(containerId)
	if err != nil {
		return err
	}
	if err := v.tool.Shutdown(domain); err != nil {
		return err
	}

	// Wait until domain is really stopped or timeout after 10 sec.
	return utils.WaitLoop(func() (bool, error) {
		domain, err := v.tool.LookupByUUIDString(containerId)
		if err != nil {
			return true, err
		}

		di, err := v.tool.GetDomainInfo(domain)
		if err != nil {
			return false, err
		}

		return di.State == libvirt.DOMAIN_SHUTDOWN, nil
	}, 10*time.Second)
}

// RemoveContainer tries to gracefully stop domain, then forcibly removes it
// even if it's still running
// it waits up to 5 sec for doing the job by libvirt
func (v *VirtualizationTool) RemoveContainer(containerId string) error {
	// Give a chance to gracefully stop domain
	// TODO: handle errors - there could be e.x. connection error
	v.StopContainer(containerId)

	domain, err := v.tool.LookupByUUIDString(containerId)
	if err != nil {
		return err
	}

	if err := v.tool.Destroy(domain); err != nil {
		return err
	}

	if err := v.tool.Undefine(domain); err != nil {
		return err
	}

	// Wait until domain is really removed or timeout after 5 sec.
	return utils.WaitLoop(func() (bool, error) {
		domain, err := v.tool.LookupByUUIDString(containerId)
		if domain != nil && err != nil {
			return false, nil
		}

		// There must be an error
		if err == nil {
			return false, errors.New("libvirt returned no domain and no error - this is incorrect")
		}

		lastLibvirtErr := v.tool.GetLastError()
		if lastLibvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return true, nil
		}

		// Other error occured
		return false, err
	}, 5*time.Second)
}

func libvirtToKubeState(domainState libvirt.DomainState, lastState kubeapi.ContainerState) kubeapi.ContainerState {
	var containerState kubeapi.ContainerState

	switch domainState {
	case libvirt.DOMAIN_RUNNING:
		containerState = kubeapi.ContainerState_CONTAINER_RUNNING
	case libvirt.DOMAIN_PAUSED:
		if lastState == kubeapi.ContainerState_CONTAINER_CREATED {
			containerState = kubeapi.ContainerState_CONTAINER_CREATED
		} else {
			containerState = kubeapi.ContainerState_CONTAINER_EXITED
		}
	case libvirt.DOMAIN_SHUTDOWN:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	case libvirt.DOMAIN_SHUTOFF:
		if lastState == kubeapi.ContainerState_CONTAINER_CREATED {
			containerState = kubeapi.ContainerState_CONTAINER_CREATED
		} else {
			containerState = kubeapi.ContainerState_CONTAINER_EXITED
		}
	case libvirt.DOMAIN_CRASHED:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	case libvirt.DOMAIN_PMSUSPENDED:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	default:
		containerState = kubeapi.ContainerState_CONTAINER_UNKNOWN
	}

	return containerState
}

func (v *VirtualizationTool) getContainerInfo(metadataStore metadata.MetadataStore, domain *libvirt.Domain, containerId string) (*metadata.ContainerInfo, error) {
	containerInfo, err := metadataStore.GetContainerInfo(containerId)
	if err != nil {
		return nil, err
	}
	if containerInfo == nil {
		return nil, nil
	}

	domainInfo, err := v.tool.GetDomainInfo(domain)
	if err != nil {
		return nil, err
	}

	containerState := libvirtToKubeState(domainInfo.State, containerInfo.State)
	if containerInfo.State != containerState {
		if err := metadataStore.UpdateState(containerId, byte(containerState)); err != nil {
			return nil, err
		}
		startedAt := time.Now().UnixNano()
		if containerState == kubeapi.ContainerState_CONTAINER_RUNNING {
			strStartedAt := strconv.FormatInt(startedAt, 10)
			if err := metadataStore.UpdateStartedAt(containerId, strStartedAt); err != nil {
				return nil, err
			}
		}
		containerInfo.StartedAt = startedAt
		containerInfo.State = containerState
	}
	return containerInfo, nil
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

func (v *VirtualizationTool) getContainer(metadataStore metadata.MetadataStore, domain *libvirt.Domain) (*kubeapi.Container, error) {
	containerId, err := v.tool.GetUUIDString(domain)
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.getContainerInfo(metadataStore, domain, containerId)
	if err != nil {
		return nil, err
	}

	podSandboxId := containerInfo.SandboxId

	metadata := &kubeapi.ContainerMetadata{
		Name: &containerId,
	}

	image := &kubeapi.ImageSpec{Image: &containerInfo.Image}

	container := &kubeapi.Container{
		Id:           &containerId,
		PodSandboxId: &podSandboxId,
		Metadata:     metadata,
		Image:        image,
		ImageRef:     &containerInfo.Image,
		State:        &containerInfo.State,
		CreatedAt:    &containerInfo.CreatedAt,
		Labels:       containerInfo.Labels,
		Annotations:  containerInfo.Annotations,
	}
	return container, nil
}

func (v *VirtualizationTool) ListContainers(metadataStore metadata.MetadataStore, filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
	containers := make([]*kubeapi.Container, 0)

	if filter != nil {
		if filter.GetId() != "" {
			// Verify if there is container metadata
			containerInfo, err := metadataStore.GetContainerInfo(filter.GetId())
			if err != nil {
				return nil, err
			}
			if containerInfo == nil {
				// There's no such container - looks like it's already removed, so return an empty list
				return containers, nil
			}

			// Query libvirt for domain found in metadata store
			// TODO: Distinguish lack of domain from other errors
			domain, err := v.tool.LookupByUUIDString(filter.GetId())
			if err != nil {
				// There's no such domain - looks like it's already removed, so return an empty list
				return containers, nil
			}
			container, err := v.getContainer(metadataStore, domain)
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
			domainID, err := metadataStore.GetPodSandboxContainerID(filter.GetPodSandboxId())
			if err != nil {
				// There's no such sandbox - looks like it's already removed, so return an empty list
				return containers, nil
			}
			// Verify if there is container metadata
			containerInfo, err := metadataStore.GetContainerInfo(domainID)
			if err != nil {
				return nil, err
			}
			if containerInfo == nil {
				// There's no such container - looks like it's already removed, but still is mentioned in sandbox
				return nil, fmt.Errorf("Container metadata not found, but it's still mentioned in sandbox %s", filter.GetPodSandboxId())
			}

			// TODO: Distinguish lack of domain from other errors
			domain, err := v.tool.LookupByUUIDString(domainID)
			if err != nil {
				// There's no such domain - looks like it's already removed, so return an empty list
				return containers, nil
			}
			container, err := v.getContainer(metadataStore, domain)
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
	domains, err := v.tool.ListAll()
	if err != nil {
		return nil, err
	}
	for _, domain := range domains {
		container, err := v.getContainer(metadataStore, &domain)
		if err != nil {
			return nil, err
		}

		if filterContainer(container, filter) {
			containers = append(containers, container)
		}
	}

	return containers, nil
}

func (v *VirtualizationTool) ContainerStatus(metadataStore metadata.MetadataStore, containerId string) (*kubeapi.ContainerStatus, error) {
	domain, err := v.tool.LookupByUUIDString(containerId)
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.getContainerInfo(metadataStore, domain, containerId)
	if err != nil {
		return nil, err
	}

	if containerInfo == nil {
		return nil, fmt.Errorf("missing containerInfo for containerId: %s", containerId)
	}

	image := &kubeapi.ImageSpec{Image: &containerInfo.Image}

	return &kubeapi.ContainerStatus{
		Id:        &containerId,
		Metadata:  &kubeapi.ContainerMetadata{},
		Image:     image,
		ImageRef:  &containerInfo.Image,
		State:     &containerInfo.State,
		CreatedAt: &containerInfo.CreatedAt,
		StartedAt: &containerInfo.StartedAt,
	}, nil
}

func (v *VirtualizationTool) RemoveVolume(name string) error {
	return v.volumeStorage.RemoveVolume(name)
}

func (v *VirtualizationTool) GetStoragePool() *StorageTool {
	return v.volumeStorage
}
