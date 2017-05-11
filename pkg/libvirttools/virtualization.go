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
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
	"k8s.io/apimachinery/pkg/fields"
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
	domainShutdownRetryInterval     = 10 * time.Second
	domainShutdownTimeout           = 60 * time.Second
	domainDestroyCheckInterval      = 500 * time.Millisecond
	domainDestroyTimeout            = 5 * time.Second
	diskLetterStr                   = "bcdefghijklmnopqrstu"

	podNameString = "@podname@"
)

var diskLetters = strings.Split(diskLetterStr, "")

type Driver struct {
	DriverName string `xml:"name,attr,omitempty"`
	DriverType string `xml:"type,attr,omitempty"`
}

type Secret struct {
	Type string `xml:"type,attr,omitempty"`
	UUID string `xml:"uuid,attr,omitempty"`
}

type Auth struct {
	Username string `xml:"username,attr,omitempty"`
	Secret   Secret `xml:"secret"`
}

type SourceHost struct {
	Name string `xml:"name,attr,omitempty"`
	Port string `xml:"port,attr,omitempty"`
}

type Source struct {
	Device   string       `xml:"dev,attr,omitempty"`
	SrcFile  string       `xml:"file,attr,omitempty"`
	Protocol string       `xml:"protocol,attr,omitempty"`
	Name     string       `xml:"name,attr,omitempty"`
	Hosts    []SourceHost `xml:"host,omitempty"`
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
	Auth       *Auth  `xml:"auth"`
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

type FlexVolumeInfo struct {
	DiskXML   string
	SecretXML string
	Key       string
}

func canUseKvm() bool {
	if os.Getenv("VIRTLET_DISABLE_KVM") != "" {
		glog.V(0).Infof("VIRTLET_DISABLE_KVM env var not empty, using plain qemu")
		return false
	}
	return true
}

func generateDomXML(useKvm bool, name string, memory int64, memoryUnit string, uuid string, cpuNum int, cpuShare int64, cpuPeriod int64, cpuQuota int64, rootDiskFilepath, netNSPath, cniConfig string) string {
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
	return fmt.Sprintf(domXML, domainType, uuid, name, uuid, memoryUnit, memory, cpuNum, cpuShare, cpuPeriod, cpuQuota, rootDiskFilepath, emulator, netNSPath, cniConfigEscaped)
}

func (v *VirtualizationTool) createBootImageClone(cloneName, imageName string) (string, error) {
	imageVolumeName, err := ImageNameToVolumeName(imageName)
	if err != nil {
		return "", err
	}

	imageVolume, err := v.imagesStorage.LookupVolume(imageVolumeName)
	if err != nil {
		return "", err
	}

	vol, err := v.volumeStorage.CloneVolume(cloneName, imageVolume)
	if err != nil {
		return "", err
	}

	return vol.GetPath()
}

func gatherFlexvolumeDriverVolumeDefinitions(podID, podName string, lettersInd int) ([]FlexVolumeInfo, error) {
	// FIXME: kubelet's --root-dir may be something other than /var/lib/kubelet
	// Need to remove it from daemonset mounts (both dev and non-dev)
	// Use 'nsenter -t 1 -m -- tar ...' or something to grab the path
	// from root namespace
	var flexInfos []FlexVolumeInfo
	dir := fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes/virtlet~flexvolume_driver", podID)
	_, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return flexInfos, nil
	} else if err != nil {
		return nil, err
	}

	glog.V(2).Info("Processing FlexVolumes for flexvolume_driver")
	vols, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	glog.V(2).Infof("Found FlexVolumes definitions at %s:\n%v", dir, vols)
	for _, vol := range vols {
		fileInfo, err := os.Stat(path.Join(dir, vol.Name()))
		if err != nil {
			return nil, err
		}
		if fileInfo.IsDir() {
			if lettersInd == len(diskLetters) {
				glog.Errorf("Had to omit attaching of one ore more flex volumes. Limit on number is: %d", lettersInd)
				return flexInfos, nil
			}
			fileInfos, err := ioutil.ReadDir(path.Join(dir, vol.Name()))
			glog.V(2).Infof("Found FlexVolume definition parts in nested dir %s:\n %v", vol.Name(), fileInfos)
			if err != nil {
				return nil, err
			}
			var flexvol FlexVolumeInfo
			for _, fileInfo := range fileInfos {
				curPath := path.Join(dir, vol.Name(), fileInfo.Name())
				if fileInfo.IsDir() {
					isoName := fileInfo.Name()
					if !strings.HasSuffix(isoName, ".cd") {
						return nil, fmt.Errorf("unexpected directory %q: must have .cd suffix", curPath)
					}
					volId := isoName[:len(isoName)-3]
					isoPath := path.Join(dir, vol.Name(), volId+".iso")
					if err := maybeInjectPodName(path.Join(curPath, "meta-data"), podName); err != nil {
						return nil, fmt.Errorf("error injecting the pod name: %v", err)
					}
					if err := utils.GenIsoImage(isoPath, volId, curPath); err != nil {
						return nil, fmt.Errorf("error generating iso image: %v", err)
					}
					continue
				}
				content, err := ioutil.ReadFile(curPath)
				if err != nil {
					return nil, err
				}
				switch fileInfo.Name() {
				case "disk.xml":
					flexvol.DiskXML = string(content)
				case "secret.xml":
					flexvol.SecretXML = string(content)
				case "key":
					flexvol.Key = string(content)
				}
			}
			flexvol.DiskXML = fmt.Sprintf(flexvol.DiskXML, "vd"+diskLetters[lettersInd])
			lettersInd++
			flexInfos = append(flexInfos, flexvol)
		}
	}
	return flexInfos, nil
}

func addDiskToDomainDefinition(domain *Domain, volXML string) error {
	disk := Disk{}
	if err := xml.Unmarshal([]byte(volXML), &disk); err != nil {
		return err
	}
	domain.Devs.DiskList = append(domain.Devs.DiskList, disk)
	return nil
}

func marshalToXML(domain *Domain) (string, error) {
	outArr, err := xml.MarshalIndent(domain, " ", "  ")
	if err != nil {
		return "", err
	}
	return string(outArr[:]), nil
}

func (v *VirtualizationTool) addAttachedVolumesXML(podID, podName, uuid, virtletVolsDesc, domXML string) (string, error) {
	glog.V(3).Infof("INPUT domain:\n%s\n\n", domXML)
	domain := &Domain{}
	err := xml.Unmarshal([]byte(domXML), domain)
	if err != nil {
		return "", err
	}

	flexVolumeInfos, err := gatherFlexvolumeDriverVolumeDefinitions(podID, podName, 0)
	if err != nil {
		return "", err
	}

	glog.V(2).Infof("FlexVolumes set to process: %v", flexVolumeInfos)
	for _, flexVolumeInfo := range flexVolumeInfos {
		if flexVolumeInfo.SecretXML != "" {
			secret, err := v.tool.DefineSecretFromXML(flexVolumeInfo.SecretXML)
			if err != nil {
				return "", err
			}
			key, err := base64.StdEncoding.DecodeString(flexVolumeInfo.Key)
			if err != nil {
				return "", err
			}
			secret.SetValue(key, 0)
		}
		if err = addDiskToDomainDefinition(domain, flexVolumeInfo.DiskXML); err != nil {
			return "", err
		}
	}

	volumesXML, err := v.volumeStorage.PrepareVolumesToBeAttached(virtletVolsDesc, uuid, len(flexVolumeInfos))
	if err != nil {
		return "", err
	}

	for _, volXML := range volumesXML {
		if err = addDiskToDomainDefinition(domain, volXML); err != nil {
			return "", err
		}
	}

	return marshalToXML(domain)
}

type VirtualizationTool struct {
	tool           DomainOperations
	volumeStorage  *StorageTool
	imagesStorage  *StorageTool
	volumePoolName string
}

func NewVirtualizationTool(conn *libvirt.Connect, volumesPoolName string, imagesStorage *StorageTool, rawDevices string) (*VirtualizationTool, error) {
	storageTool, err := NewStorageTool(conn, volumesPoolName, rawDevices)
	if err != nil {
		return nil, err
	}
	tool := NewLibvirtDomainOperations(conn)
	return &VirtualizationTool{tool: tool, volumeStorage: storageTool, imagesStorage: imagesStorage}, nil
}

func (v *VirtualizationTool) CreateContainer(metadataStore metadata.MetadataStore, in *kubeapi.CreateContainerRequest, imageName string, netNSPath, cniConfig string) (string, error) {
	uuid, err := utils.NewUuid()
	if err != nil {
		return "", err
	}

	if in.Config == nil || in.Config.Metadata == nil || in.Config.Image == nil || in.SandboxConfig == nil || in.SandboxConfig.Metadata == nil {
		return "", errors.New("invalid input data")
	}

	config := in.Config
	name := config.Metadata.Name
	sandBoxAnnotations, err := metadataStore.GetPodSandboxAnnotations(in.PodSandboxId)
	if err != nil {
		return "", err
	}

	if name == "" {
		name = uuid
	} else {
		// check whether the domain with such name already exists, if so - return it's uuid
		domainName := in.PodSandboxId + "-" + name
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

	cloneName := "root_" + uuid
	cloneImage, err := v.createBootImageClone(cloneName, imageName)
	if err != nil {
		return "", err
	}

	metadataStore.SetContainer(name, uuid, in.PodSandboxId, config.Image.Image, cloneName, config.Labels, config.Annotations)

	var memory, cpuShares, cpuPeriod, cpuQuota int64
	if config.Linux != nil && config.Linux.Resources != nil {
		memory = config.Linux.Resources.MemoryLimitInBytes
		cpuShares = config.Linux.Resources.CpuShares
		cpuPeriod = config.Linux.Resources.CpuPeriod
		cpuQuota = config.Linux.Resources.CpuQuota
	}
	memoryUnit := "b"
	if memory == 0 {
		memory = defaultMemory
		memoryUnit = defaultMemoryUnit
	}

	cpuNum, err := utils.GetvCPUsNum()
	if err != nil {
		return "", err
	}

	domXML := generateDomXML(canUseKvm(), name, memory, memoryUnit, uuid, cpuNum, cpuShares, cpuPeriod, cpuQuota, cloneImage, netNSPath, cniConfig)

	virtletVolsDesc, _ := sandBoxAnnotations[VirtletVolumesAnnotationKeyName]
	domXML, err = v.addAttachedVolumesXML(in.PodSandboxId, in.SandboxConfig.Metadata.Name, uuid, virtletVolsDesc, domXML)
	if err != nil {
		return "", err
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

	// To process cases when VM is booting and cannot handle the shutdown signal
	// send sequential shutdown requests with 1 sec interval until domain is shutoff or time after 60 sec.
	return utils.WaitLoop(func() (bool, error) {
		v.tool.Shutdown(domain)

		domain, err := v.tool.LookupByUUIDString(containerId)
		if err != nil {
			return true, err
		}

		di, err := v.tool.GetDomainInfo(domain)
		if err != nil {
			return false, err
		}
		return di.State == libvirt.DOMAIN_SHUTDOWN || di.State == libvirt.DOMAIN_SHUTOFF, nil
	}, domainShutdownRetryInterval, domainShutdownTimeout)
}

// RemoveContainer tries to gracefully stop domain, then forcibly removes it
// even if it's still running
// it waits up to 5 sec for doing the job by libvirt
func (v *VirtualizationTool) RemoveContainer(containerId string) error {
	// Give a chance to gracefully stop domain
	// TODO: handle errors - there could be e.x. connection errori

	domain, err := v.tool.LookupByUUIDString(containerId)
	if err != nil {
		return err
	}

	if err := v.StopContainer(containerId); err != nil {
		if err := v.tool.Destroy(domain); err != nil {
			return err
		}
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

		lastLibvirtErr, ok := err.(libvirt.Error)
		if !ok {
			return false, errors.New("Failed to cast error to libvirt.Error type")
		}
		if lastLibvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return true, nil
		}

		// Other error occured
		return false, err
	}, domainDestroyCheckInterval, domainDestroyTimeout)
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
		if filter.Id != "" && container.Id != filter.Id {
			return false
		}

		if filter.PodSandboxId != "" && container.PodSandboxId != filter.PodSandboxId {
			return false
		}

		if filter.State != nil && container.State != filter.GetState().State {
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
		Name: containerId,
	}

	image := &kubeapi.ImageSpec{Image: containerInfo.Image}

	container := &kubeapi.Container{
		Id:           containerId,
		PodSandboxId: podSandboxId,
		Metadata:     metadata,
		Image:        image,
		ImageRef:     containerInfo.Image,
		State:        containerInfo.State,
		CreatedAt:    containerInfo.CreatedAt,
		Labels:       containerInfo.Labels,
		Annotations:  containerInfo.Annotations,
	}
	return container, nil
}

func (v *VirtualizationTool) ListContainers(metadataStore metadata.MetadataStore, filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
	containers := make([]*kubeapi.Container, 0)

	if filter != nil {
		if filter.Id != "" {
			// Verify if there is container metadata
			containerInfo, err := metadataStore.GetContainerInfo(filter.Id)
			if err != nil {
				return nil, err
			}
			if containerInfo == nil {
				// There's no such container - looks like it's already removed, so return an empty list
				return containers, nil
			}

			// Query libvirt for domain found in metadata store
			// TODO: Distinguish lack of domain from other errors
			domain, err := v.tool.LookupByUUIDString(filter.Id)
			if err != nil {
				// There's no such domain - looks like it's already removed, so return an empty list
				return containers, nil
			}
			container, err := v.getContainer(metadataStore, domain)
			if err != nil {
				return nil, err
			}

			if filter.PodSandboxId != "" && container.PodSandboxId != filter.PodSandboxId {
				return containers, nil
			}
			if filterContainer(container, filter) {
				containers = append(containers, container)
			}
			return containers, nil
		} else if filter.PodSandboxId != "" {
			domainID, err := metadataStore.GetPodSandboxContainerID(filter.PodSandboxId)
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
				return nil, fmt.Errorf("Container metadata not found, but it's still mentioned in sandbox %s", filter.PodSandboxId)
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

	image := &kubeapi.ImageSpec{Image: containerInfo.Image}

	return &kubeapi.ContainerStatus{
		Id: containerId,
		Metadata: &kubeapi.ContainerMetadata{
			Name:    containerInfo.Name,
			Attempt: 0,
		},
		Image:     image,
		ImageRef:  containerInfo.Image,
		State:     containerInfo.State,
		CreatedAt: containerInfo.CreatedAt,
		StartedAt: containerInfo.StartedAt,
	}, nil
}

func (v *VirtualizationTool) RemoveVolume(name string) error {
	return v.volumeStorage.RemoveVolume(name)
}

func (v *VirtualizationTool) GetStoragePool() *StorageTool {
	return v.volumeStorage
}

func maybeInjectPodName(targetFile, podName string) error {
	content, err := ioutil.ReadFile(targetFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %q: %v", targetFile, err)
	}
	toFind := []byte(podNameString)
	if !bytes.Contains(content, toFind) {
		return nil
	}
	content = bytes.Replace(content, toFind, []byte(podName), -1)
	if err := ioutil.WriteFile(targetFile, content, 0644); err != nil {
		return fmt.Errorf("writing %q: %v", targetFile, err)
	}
	return nil
}
