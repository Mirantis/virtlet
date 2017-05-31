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
	libvirtxml "github.com/libvirt/libvirt-go-xml"
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

	domainShutdownRetryInterval = 10 * time.Second
	domainShutdownTimeout       = 60 * time.Second
	domainDestroyCheckInterval  = 500 * time.Millisecond
	domainDestroyTimeout        = 5 * time.Second
	diskLetterStr               = "bcdefghijklmnopqrstu"

	podNameString = "@podname@"
)

var diskLetters = strings.Split(diskLetterStr, "")

type FlexVolumeInfo struct {
	Disk      *libvirtxml.DomainDisk
	SecretXML string
	Key       string
}

type VirtletDomainSettings struct {
	useKvm           bool
	domainName       string
	domainUUID       string
	memory           int
	memoryUnit       string
	vcpuNum          int
	cpuShares        uint
	cpuPeriod        uint64
	cpuQuota         int64
	rootDiskFilepath string
	netNSPath        string
	cniConfig        string
}

func canUseKvm() bool {
	if os.Getenv("VIRTLET_DISABLE_KVM") != "" {
		glog.V(0).Infof("VIRTLET_DISABLE_KVM env var not empty, using plain qemu")
		return false
	}
	return true
}

func (ds *VirtletDomainSettings) createDomain() *libvirtxml.Domain {
	domainType := defaultDomainType
	emulator := defaultEmulator
	if !ds.useKvm {
		domainType = noKvmDomainType
		emulator = noKvmEmulator
	}

	domain := &libvirtxml.Domain{

		Devices: &libvirtxml.DomainDeviceList{
			Emulator: "/vmwrapper",
			Inputs:   []libvirtxml.DomainInput{libvirtxml.DomainInput{Type: "tablet", Bus: "usb"}},
			Graphics: []libvirtxml.DomainGraphic{libvirtxml.DomainGraphic{Type: "vnc", Port: -1}},
			Serials:  []libvirtxml.DomainChardev{libvirtxml.DomainChardev{Type: "pty", Target: &libvirtxml.DomainChardevTarget{Port: "0"}}},
			Consoles: []libvirtxml.DomainChardev{libvirtxml.DomainChardev{Type: "pty", Target: &libvirtxml.DomainChardevTarget{Type: "serial", Port: "0"}}},
			Videos:   []libvirtxml.DomainVideo{libvirtxml.DomainVideo{Model: libvirtxml.DomainVideoModel{Type: "cirrus"}}},
			Disks: []libvirtxml.DomainDisk{libvirtxml.DomainDisk{
				Type:   "file",
				Device: "disk",
				Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "qcow2"},
				Source: &libvirtxml.DomainDiskSource{File: ds.rootDiskFilepath},
				Target: &libvirtxml.DomainDiskTarget{Dev: "vda", Bus: "virtio"},
			}},
		},

		OS: &libvirtxml.DomainOS{
			Type:        &libvirtxml.DomainOSType{Type: "hvm"},
			BootDevices: []libvirtxml.DomainBootDevice{libvirtxml.DomainBootDevice{Dev: "hd"}},
		},

		Features: &libvirtxml.DomainFeatureList{ACPI: &libvirtxml.DomainFeature{}},

		OnPoweroff: "destroy",
		OnReboot:   "restart",
		OnCrash:    "restart",

		Type: domainType,

		Name:   ds.domainUUID + "-" + ds.domainName,
		UUID:   ds.domainUUID,
		Memory: &libvirtxml.DomainMemory{Value: ds.memory, Unit: ds.memoryUnit},
		VCPU:   &libvirtxml.DomainVCPU{Value: ds.vcpuNum},
		CPUTune: &libvirtxml.DomainCPUTune{
			Shares: &libvirtxml.DomainCPUTuneShares{Value: ds.cpuShares},
			Period: &libvirtxml.DomainCPUTunePeriod{Value: ds.cpuPeriod},
			Quota:  &libvirtxml.DomainCPUTuneQuota{Value: ds.cpuQuota},
		},
		MemoryBacking: &libvirtxml.DomainMemoryBacking{Locked: &libvirtxml.DomainMemoryBackingLocked{}},

		CMDLine: &libvirtxml.DomainCMDLine{
			Envs: []libvirtxml.QemuEnv{
				libvirtxml.QemuEnv{Name: "VIRTLET_EMULATOR", Value: emulator},
				libvirtxml.QemuEnv{Name: "VIRTLET_NS", Value: ds.netNSPath},
				libvirtxml.QemuEnv{Name: "VIRTLET_CNI_CONFIG", Value: ds.cniConfig},
			},
		},
	}
	return domain
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
					disk := libvirtxml.DomainDisk{}
					if err := xml.Unmarshal(content, &disk); err != nil {
						return nil, err
					}
					flexvol.Disk = &disk
				case "secret.xml":
					flexvol.SecretXML = string(content)
				case "key":
					flexvol.Key = string(content)
				}
			}
			flexvol.Disk.Target.Dev = "vd" + diskLetters[lettersInd]
			lettersInd++
			flexInfos = append(flexInfos, flexvol)
		}
	}
	return flexInfos, nil
}

func (v *VirtualizationTool) addVolumesToDomain(podID, podName string, domain *libvirtxml.Domain, virtletVols []*VirtletVolume) error {
	flexVolumeInfos, err := gatherFlexvolumeDriverVolumeDefinitions(podID, podName, 0)
	if err != nil {
		return err
	}

	glog.V(2).Infof("FlexVolumes set to process: %v", flexVolumeInfos)
	for _, flexVolumeInfo := range flexVolumeInfos {
		if flexVolumeInfo.SecretXML != "" {
			secret, err := v.tool.DefineSecretFromXML(flexVolumeInfo.SecretXML)
			if err != nil {
				return err
			}
			key, err := base64.StdEncoding.DecodeString(flexVolumeInfo.Key)
			if err != nil {
				return err
			}
			secret.SetValue(key, 0)
		}
		domain.Devices.Disks = append(domain.Devices.Disks, *flexVolumeInfo.Disk)
	}

	volumes, err := v.volumeStorage.PrepareVolumesToBeAttached(virtletVols, domain.UUID, len(flexVolumeInfos))
	if err != nil {
		return err
	}

	domain.Devices.Disks = append(domain.Devices.Disks, volumes...)
	return nil
}

type VirtualizationTool struct {
	tool           DomainOperations
	volumeStorage  *StorageTool
	imagesStorage  *StorageTool
	volumePoolName string
	metadataStore  metadata.MetadataStore
}

func NewVirtualizationTool(conn *libvirt.Connect, volumesPoolName string, imagesStorage *StorageTool, rawDevices string, metadataStore metadata.MetadataStore) (*VirtualizationTool, error) {
	storageTool, err := NewStorageTool(conn, volumesPoolName, rawDevices)
	if err != nil {
		return nil, err
	}
	tool := NewLibvirtDomainOperations(conn)
	return &VirtualizationTool{
		tool:          tool,
		volumeStorage: storageTool,
		imagesStorage: imagesStorage,
		metadataStore: metadataStore,
	}, nil
}

func (v *VirtualizationTool) CreateContainer(in *kubeapi.CreateContainerRequest, imageName string, netNSPath, cniConfig string) (string, error) {
	uuid, err := utils.NewUuid()
	if err != nil {
		return "", err
	}

	settings := VirtletDomainSettings{
		domainUUID: uuid,
		netNSPath:  netNSPath,
		cniConfig:  cniConfig,
	}

	if in.Config == nil || in.Config.Metadata == nil || in.Config.Image == nil || in.SandboxConfig == nil || in.SandboxConfig.Metadata == nil {
		return "", errors.New("invalid input data")
	}

	config := in.Config
	settings.domainName = config.Metadata.Name
	sandboxAnnotations, err := v.metadataStore.GetPodSandboxAnnotations(in.PodSandboxId)
	if err != nil {
		return "", err
	}

	if settings.domainName == "" {
		settings.domainName = settings.domainUUID
	} else {
		// check whether the domain with such name already exists, if so - return it's uuid
		domainName := in.PodSandboxId + "-" + settings.domainName
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

	cloneName := "root_" + settings.domainUUID
	settings.rootDiskFilepath, err = v.createBootImageClone(cloneName, imageName)
	if err != nil {
		return "", err
	}

	annotations, err := LoadAnnotations(sandboxAnnotations)
	if err != nil {
		return "", err
	}
	settings.vcpuNum = annotations.VCPUCount

	v.metadataStore.SetContainer(settings.domainName, settings.domainUUID, in.PodSandboxId, config.Image.Image, cloneName, config.Labels, config.Annotations)

	if config.Linux != nil && config.Linux.Resources != nil {
		settings.memory = int(config.Linux.Resources.MemoryLimitInBytes)
		settings.cpuShares = uint(config.Linux.Resources.CpuShares)
		settings.cpuPeriod = uint64(config.Linux.Resources.CpuPeriod)
		// Specified cpu bandwidth limits for domains actually are set equal per each vCPU by libvirt
		// Thus, to limit overall VM's cpu threads consumption by set value in pod definition need to perform division
		settings.cpuQuota = config.Linux.Resources.CpuQuota / int64(settings.vcpuNum)
	}
	settings.memoryUnit = "b"
	if settings.memory == 0 {
		settings.memory = defaultMemory
		settings.memoryUnit = defaultMemoryUnit
	}

	settings.useKvm = canUseKvm()
	domain := settings.createDomain()

	if err = v.addVolumesToDomain(in.PodSandboxId, in.SandboxConfig.Metadata.Name, domain, annotations.Volumes); err != nil {
		return "", err
	}

	domainXML, err := domain.Marshal()
	if err != nil {
		return "", err
	}

	glog.V(2).Infof("Creating domain:\n%s", domainXML)
	if _, err := v.tool.DefineFromXML(domainXML); err != nil {
		return "", err
	}

	domainPtr, err := v.tool.LookupByUUIDString(settings.domainUUID)
	if err != nil {
		return "", err
	}

	if _, err := v.tool.GetDomainInfo(domainPtr); err != nil {
		return "", err
	}

	return settings.domainUUID, nil
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
	containerInfo, err := v.metadataStore.GetContainerInfo(containerId)
	if err != nil {
		glog.Errorf("Error when retrieving container '%s' info from metadata store: %v", containerId, err)
		return err
	}

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
	if err := utils.WaitLoop(func() (bool, error) {
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
	}, domainDestroyCheckInterval, domainDestroyTimeout); err != nil {
		return err
	}

	if err := v.metadataStore.RemoveContainer(containerId); err != nil {
		glog.Errorf("Error when removing container '%s' from metadata store: %v", containerId, err)
		return err
	}

	if err := v.RemoveVolume(containerInfo.RootImageVolumeName); err != nil {
		glog.Errorf("Error when removing image snapshot with name '%s': %v", containerInfo.RootImageVolumeName, err)
		return err
	}

	annotations, err := LoadAnnotations(containerInfo.SandBoxAnnotations)
	if err != nil {
		return err
	}
	if len(annotations.Volumes) > 0 {
		if err := v.volumeStorage.CleanAttachedQCOW2Volumes(annotations.Volumes, containerId); err != nil {
			return err
		}
	}

	return nil
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

func (v *VirtualizationTool) getContainerInfo(domain *libvirt.Domain, containerId string) (*metadata.ContainerInfo, error) {
	containerInfo, err := v.metadataStore.GetContainerInfo(containerId)
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
		if err := v.metadataStore.UpdateState(containerId, byte(containerState)); err != nil {
			return nil, err
		}
		startedAt := time.Now().UnixNano()
		if containerState == kubeapi.ContainerState_CONTAINER_RUNNING {
			strStartedAt := strconv.FormatInt(startedAt, 10)
			if err := v.metadataStore.UpdateStartedAt(containerId, strStartedAt); err != nil {
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

func (v *VirtualizationTool) getContainer(domain *libvirt.Domain) (*kubeapi.Container, error) {
	containerId, err := v.tool.GetUUIDString(domain)
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.getContainerInfo(domain, containerId)
	if err != nil {
		return nil, err
	}

	if containerInfo == nil {
		return nil, nil
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

func (v *VirtualizationTool) ListContainers(filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
	containers := make([]*kubeapi.Container, 0)

	if filter != nil {
		if filter.Id != "" {
			// Verify if there is container metadata
			containerInfo, err := v.metadataStore.GetContainerInfo(filter.Id)
			if err != nil {
				return nil, err
			}
			if containerInfo == nil {
				// There's no such container - looks like it's already removed or has been defined externally, so return an empty list
				return containers, nil
			}

			// Query libvirt for domain found in metadata store
			// TODO: Distinguish lack of domain from other errors
			domain, err := v.tool.LookupByUUIDString(filter.Id)
			if err != nil {
				// There's no such domain - looks like it's already removed, so return an empty list
				return containers, nil
			}
			container, err := v.getContainer(domain)
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
			domainID, err := v.metadataStore.GetPodSandboxContainerID(filter.PodSandboxId)
			if err != nil {
				// There's no such sandbox - looks like it's already removed, so return an empty list
				return containers, nil
			}
			// Verify if there is container metadata
			containerInfo, err := v.metadataStore.GetContainerInfo(domainID)
			if err != nil {
				return nil, err
			}
			if containerInfo == nil {
				// There's no such container - looks like it's already removed or has been defined externally, but still is mentioned in sandbox
				return nil, fmt.Errorf("Container metadata not found, but it's still mentioned in sandbox %s", filter.PodSandboxId)
			}

			// TODO: Distinguish lack of domain from other errors
			domain, err := v.tool.LookupByUUIDString(domainID)
			if err != nil {
				// There's no such domain - looks like it's already removed, so return an empty list
				return containers, nil
			}
			container, err := v.getContainer(domain)
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
		container, err := v.getContainer(&domain)
		if err != nil {
			return nil, err
		}

		if container != nil && filterContainer(container, filter) {
			containers = append(containers, container)
		}
	}

	return containers, nil
}

func (v *VirtualizationTool) ContainerStatus(containerId string) (*kubeapi.ContainerStatus, error) {
	domain, err := v.tool.LookupByUUIDString(containerId)
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.getContainerInfo(domain, containerId)
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
