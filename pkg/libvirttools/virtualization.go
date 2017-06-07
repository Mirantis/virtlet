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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"k8s.io/apimachinery/pkg/fields"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
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
	minVirtioBlockDevChar       = 'a'
	// https://access.redhat.com/documentation/en-US/Red_Hat_Enterprise_Linux/5/html/Virtualization/sect-Virtualization-Virtualization_limitations-KVM_limitations.html
	// Actually there can be more than 21 block devices (including
	// the root volume), but we want to be on the safe side here
	maxVirtioBlockDevChar = 'u'

	ContainerNsUuid       = "67b7fb47-7735-4b64-86d2-6d062d121966"
	defaultKubeletRootDir = "/var/lib/kubelet/pods"
	vmLogLocationPty      = "pty"
)

type domainSettings struct {
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
	vmLogLocation    string
}

func (ds *domainSettings) createDomain() *libvirtxml.Domain {
	domainType := defaultDomainType
	emulator := defaultEmulator
	if !ds.useKvm {
		domainType = noKvmDomainType
		emulator = noKvmEmulator
	}

	return &libvirtxml.Domain{
		Devices: &libvirtxml.DomainDeviceList{
			Emulator: "/vmwrapper",
			Inputs:   []libvirtxml.DomainInput{libvirtxml.DomainInput{Type: "tablet", Bus: "usb"}},
			Graphics: []libvirtxml.DomainGraphic{libvirtxml.DomainGraphic{Type: "vnc", Port: -1}},
			Videos:   []libvirtxml.DomainVideo{libvirtxml.DomainVideo{Model: libvirtxml.DomainVideoModel{Type: "cirrus"}}},
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

		Name:   ds.domainName,
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
}

func canUseKvm() bool {
	if os.Getenv("VIRTLET_DISABLE_KVM") != "" {
		glog.V(0).Infof("VIRTLET_DISABLE_KVM env var not empty, using plain qemu")
		return false
	}
	return true
}

type VirtualizationTool struct {
	domainConn     virt.VirtDomainConnection
	volumePool     virt.VirtStoragePool
	imageManager   ImageManager
	metadataStore  metadata.MetadataStore
	timeFunc       func() time.Time
	forceKVM       bool
	kubeletRootDir string
	rawDevices     []string
	volumeSource   VMVolumeSource
}

var _ VolumeOwner = &VirtualizationTool{}

func NewVirtualizationTool(domainConn virt.VirtDomainConnection, storageConn virt.VirtStorageConnection, imageManager ImageManager, metadataStore metadata.MetadataStore, volumePoolName, rawDevices string, volumeSource VMVolumeSource) (*VirtualizationTool, error) {
	volumePool, err := ensureStoragePool(storageConn, volumePoolName)
	if err != nil {
		return nil, err
	}
	return &VirtualizationTool{
		domainConn:    domainConn,
		volumePool:    volumePool,
		imageManager:  imageManager,
		metadataStore: metadataStore,
		timeFunc:      time.Now,
		// FIXME: kubelet's --root-dir may be something other than /var/lib/kubelet
		// Need to remove it from daemonset mounts (both dev and non-dev)
		// Use 'nsenter -t 1 -m -- tar ...' or something to grab the path
		// from root namespace
		kubeletRootDir: defaultKubeletRootDir,
		rawDevices:     strings.Split(rawDevices, ","),
		volumeSource:   volumeSource,
	}, nil
}

func (v *VirtualizationTool) SetForceKVM(forceKVM bool) {
	v.forceKVM = forceKVM
}

func (v *VirtualizationTool) SetTimeFunc(timeFunc func() time.Time) {
	v.timeFunc = timeFunc
}

func (v *VirtualizationTool) SetKubeletRootDir(kubeletRootDir string) {
	v.kubeletRootDir = kubeletRootDir
}

func (v *VirtualizationTool) getVMVolumes(config *VMConfig) ([]VMVolume, error) {
	return v.volumeSource(config, v)
}

func (v *VirtualizationTool) setupVolumes(config *VMConfig, domainDef *libvirtxml.Domain) error {
	vmVols, err := v.getVMVolumes(config)
	if err != nil {
		return err
	}
	for n, vmVol := range vmVols {
		diskChar := minVirtioBlockDevChar + n
		if diskChar > maxVirtioBlockDevChar {
			return fmt.Errorf("too many block devices")
		}
		virtDev := fmt.Sprintf("vd%c", diskChar)
		diskDef, err := vmVol.Setup(virtDev)
		if err != nil {
			// try to tear down volumes that were already set up
			for _, vmVol := range vmVols[:n] {
				if err := vmVol.Teardown(); err != nil {
					glog.Warning("failed to tear down a volume on error: %v", err)
				}
			}
			return err
		}
		domainDef.Devices.Disks = append(domainDef.Devices.Disks, *diskDef)
	}
	return nil
}

func (v *VirtualizationTool) teardownVolumes(config *VMConfig) error {
	vmVols, err := v.getVMVolumes(config)
	if err != nil {
		return err
	}
	var errs []string
	for _, vmVol := range vmVols {
		if err := vmVol.Teardown(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if errs != nil {
		return fmt.Errorf("failed to tear down some of the volumes:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

func vmLogLocation() string {
	logLocation := os.Getenv("VIRTLET_VM_LOG_LOCATION")
	if logLocation == "" {
		return vmLogLocationPty
	}
	return logLocation
}

func (v *VirtualizationTool) RemoveLibvirtSandboxLog(sandboxId string) error {
	logLocation := vmLogLocation()
	if logLocation == vmLogLocationPty {
		return nil
	}
	return os.RemoveAll(filepath.Join(logLocation, sandboxId))
}

func (v *VirtualizationTool) addSerialDevicesToDomain(sandboxId string, containerAttempt uint32, domain *libvirtxml.Domain, settings domainSettings) error {
	if settings.vmLogLocation != vmLogLocationPty {
		logDir := filepath.Join(settings.vmLogLocation, sandboxId)
		logPath := filepath.Join(logDir, fmt.Sprintf("_%d.log", containerAttempt))

		// Prepare directory where libvirt will store log file to.
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			if err := os.Mkdir(logDir, 0644); err != nil {
				return fmt.Errorf("failed to create vmLogDir '%s': %s", logDir, err.Error())
			}
		}

		domain.Devices.Serials = []libvirtxml.DomainChardev{
			libvirtxml.DomainChardev{
				Type:   "file",
				Target: &libvirtxml.DomainChardevTarget{Port: "0"},
				Source: &libvirtxml.DomainChardevSource{Path: logPath},
			},
		}
		domain.Devices.Consoles = []libvirtxml.DomainChardev{
			libvirtxml.DomainChardev{
				Type:   "file",
				Target: &libvirtxml.DomainChardevTarget{Type: "serial", Port: "0"},
				Source: &libvirtxml.DomainChardevSource{Path: logPath},
			},
		}
	} else {
		domain.Devices.Serials = []libvirtxml.DomainChardev{
			libvirtxml.DomainChardev{
				Type:   "pty",
				Target: &libvirtxml.DomainChardevTarget{Port: "0"},
			},
		}
		domain.Devices.Consoles = []libvirtxml.DomainChardev{
			libvirtxml.DomainChardev{
				Type:   "pty",
				Target: &libvirtxml.DomainChardevTarget{Type: "serial", Port: "0"},
			},
		}
	}
	return nil
}

func (v *VirtualizationTool) CreateContainer(config *VMConfig, netNSPath, cniConfig string) (string, error) {
	if err := config.LoadAnnotations(); err != nil {
		return "", err
	}

	domainUUID := utils.NewUuid5(ContainerNsUuid, config.PodSandboxId)
	// FIXME: this field should be moved to VMStatus struct (to be added)
	config.DomainUUID = domainUUID
	settings := domainSettings{
		domainUUID:    domainUUID,
		domainName:    domainUUID + "-" + config.Name,
		netNSPath:     netNSPath,
		cniConfig:     cniConfig,
		vmLogLocation: vmLogLocation(),
	}

	cloneName := "root_" + settings.domainUUID
	settings.vcpuNum = config.ParsedAnnotations.VCPUCount
	settings.memory = int(config.MemoryLimitInBytes)
	settings.cpuShares = uint(config.CpuShares)
	settings.cpuPeriod = uint64(config.CpuPeriod)
	// Specified cpu bandwidth limits for domains actually are set equal per each vCPU by libvirt
	// Thus, to limit overall VM's cpu threads consumption by set value in pod definition need to perform division
	settings.cpuQuota = config.CpuQuota / int64(settings.vcpuNum)
	settings.memoryUnit = "b"
	if settings.memory == 0 {
		settings.memory = defaultMemory
		settings.memoryUnit = defaultMemoryUnit
	}

	settings.useKvm = v.forceKVM || canUseKvm()
	domainConf := settings.createDomain()

	if err := v.setupVolumes(config, domainConf); err != nil {
		return "", err
	}

	ok := false
	defer func() {
		if ok {
			return
		}
		if err := v.removeDomain(settings.domainUUID, config); err != nil {
			glog.Warning("failed to remove domain: %v", err)
		}
	}()

	containerAttempt := config.Attempt
	if err := v.addSerialDevicesToDomain(config.PodSandboxId, containerAttempt, domainConf, settings); err != nil {
		return "", err
	}

	if _, err := v.domainConn.DefineDomain(domainConf); err != nil {
		return "", err
	}

	domain, err := v.domainConn.LookupDomainByUUIDString(settings.domainUUID)
	if err == nil {
		// FIXME: do we really need this?
		// (this causes an GetInfo() call on the domain in case of libvirt)
		_, err = domain.State()
	}

	if err == nil {
		// FIXME: store VMConfig + VMStatus (to be added)
		nocloudFile := config.TempFile
		err = v.metadataStore.SetContainer(config.Name, settings.domainUUID, config.PodSandboxId, config.Image, cloneName, config.ContainerLabels, config.ContainerAnnotations, nocloudFile, v.timeFunc)
	}
	if err != nil {
		return "", err
	}

	ok = true
	return settings.domainUUID, nil
}

func (v *VirtualizationTool) StartContainer(containerId string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerId)
	if err == nil {
		err = domain.Create()
	}

	if err != nil {
		if rmErr := v.RemoveContainer(containerId); rmErr != nil {
			return fmt.Errorf("Container start error: %v \n %v", err, rmErr)
		} else {
			return err
		}
	}

	return nil
}

func (v *VirtualizationTool) StopContainer(containerId string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerId)
	if err != nil {
		return err
	}

	// To process cases when VM is booting and cannot handle the shutdown signal
	// send sequential shutdown requests with 1 sec interval until domain is shutoff or time after 60 sec.
	return utils.WaitLoop(func() (bool, error) {
		domain.Shutdown()

		_, err := v.domainConn.LookupDomainByUUIDString(containerId)
		if err != nil {
			return true, err
		}

		state, err := domain.State()
		if err != nil {
			return false, err
		}
		return state == virt.DOMAIN_SHUTDOWN || state == virt.DOMAIN_SHUTOFF, nil
	}, domainShutdownRetryInterval, domainShutdownTimeout)
}

func (v *VirtualizationTool) removeDomain(containerId string, config *VMConfig) error {
	// Give a chance to gracefully stop domain
	// TODO: handle errors - there could be e.x. connection errori
	domain, err := v.domainConn.LookupDomainByUUIDString(containerId)
	if err != nil && err != virt.ErrDomainNotFound {
		return err
	}

	if domain != nil {
		if err := v.StopContainer(containerId); err != nil {
			if err := domain.Destroy(); err != nil {
				return err
			}
		}

		if err := domain.Undefine(); err != nil {
			return err
		}
	}

	// Wait until domain is really removed or timeout after 5 sec.
	if err := utils.WaitLoop(func() (bool, error) {
		if _, err := v.domainConn.LookupDomainByUUIDString(containerId); err == virt.ErrDomainNotFound {
			return true, nil
		} else if err != nil {
			// Unexpected error occured
			return false, fmt.Errorf("error looking up domain %q: %v", containerId, err)
		}
		return false, nil
	}, domainDestroyCheckInterval, domainDestroyTimeout); err != nil {
		return err
	}

	if err := v.teardownVolumes(config); err != nil {
		return err
	}

	return nil
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
	if containerInfo == nil {
		// the vm is already removed
		return nil
	}

	// TODO: here we're using incomplete VMConfig to tear down the volumes
	// What actually needs to be done is storing VMConfig and VMStatus (to be added)
	config := &VMConfig{
		PodSandboxId:         containerInfo.SandboxId,
		Name:                 containerInfo.Name,
		Image:                containerInfo.Image,
		DomainUUID:           containerId,
		PodAnnotations:       containerInfo.SandBoxAnnotations,
		ContainerAnnotations: containerInfo.Annotations,
		ContainerLabels:      containerInfo.Labels,
		TempFile:             containerInfo.NocloudFile,
	}
	if err := config.LoadAnnotations(); err != nil {
		return err
	}

	if err := v.removeDomain(containerId, config); err != nil {
		return err
	}

	if v.metadataStore.RemoveContainer(containerId); err != nil {
		glog.Errorf("Error when removing container '%s' from metadata store: %v", containerId, err)
		return err
	}

	return nil
}

func virtToKubeState(domainState virt.DomainState, lastState kubeapi.ContainerState) kubeapi.ContainerState {
	var containerState kubeapi.ContainerState

	switch domainState {
	case virt.DOMAIN_RUNNING:
		containerState = kubeapi.ContainerState_CONTAINER_RUNNING
	case virt.DOMAIN_PAUSED:
		if lastState == kubeapi.ContainerState_CONTAINER_CREATED {
			containerState = kubeapi.ContainerState_CONTAINER_CREATED
		} else {
			containerState = kubeapi.ContainerState_CONTAINER_EXITED
		}
	case virt.DOMAIN_SHUTDOWN:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	case virt.DOMAIN_SHUTOFF:
		if lastState == kubeapi.ContainerState_CONTAINER_CREATED {
			containerState = kubeapi.ContainerState_CONTAINER_CREATED
		} else {
			containerState = kubeapi.ContainerState_CONTAINER_EXITED
		}
	case virt.DOMAIN_CRASHED:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	case virt.DOMAIN_PMSUSPENDED:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	default:
		containerState = kubeapi.ContainerState_CONTAINER_UNKNOWN
	}

	return containerState
}

func (v *VirtualizationTool) getContainerInfo(domain virt.VirtDomain, containerId string) (*metadata.ContainerInfo, error) {
	containerInfo, err := v.metadataStore.GetContainerInfo(containerId)
	if err != nil {
		return nil, err
	}
	if containerInfo == nil {
		return nil, nil
	}

	state, err := domain.State()
	if err != nil {
		return nil, err
	}

	containerState := virtToKubeState(state, containerInfo.State)
	if containerInfo.State != containerState {
		if err := v.metadataStore.UpdateState(containerId, byte(containerState)); err != nil {
			return nil, err
		}
		startedAt := v.timeFunc().UnixNano()
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

func (v *VirtualizationTool) getContainer(domain virt.VirtDomain) (*kubeapi.Container, error) {
	containerId, err := domain.UUIDString()
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
				// There's no such container - looks like it's already removed, so return an empty list
				return containers, nil
			}

			// Query libvirt for domain found in metadata store
			// TODO: Distinguish lack of domain from other errors
			domain, err := v.domainConn.LookupDomainByUUIDString(filter.Id)
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
				// There's no such container - looks like it's already removed, but still is mentioned in sandbox
				return nil, fmt.Errorf("Container metadata not found, but it's still mentioned in sandbox %s", filter.PodSandboxId)
			}

			// TODO: Distinguish lack of domain from other errors
			domain, err := v.domainConn.LookupDomainByUUIDString(domainID)
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
	domains, err := v.domainConn.ListDomains()
	if err != nil {
		return nil, err
	}
	for _, domain := range domains {
		container, err := v.getContainer(domain)
		if err != nil {
			return nil, err
		}

		if container == nil {
			containerId, err := domain.UUIDString()
			if err != nil {
				return nil, err
			}
			glog.V(0).Infof("Failed to find info in bolt for domain with id: %s, so just ignoring as not handled by virtlet.", containerId)
			continue
		}

		if filterContainer(container, filter) {
			containers = append(containers, container)
		}
	}

	return containers, nil
}

func (v *VirtualizationTool) ContainerStatus(containerId string) (*kubeapi.ContainerStatus, error) {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerId)
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

// VolumeOwner implementation follows

func (v *VirtualizationTool) StoragePool() virt.VirtStoragePool           { return v.volumePool }
func (v *VirtualizationTool) DomainConnection() virt.VirtDomainConnection { return v.domainConn }
func (v *VirtualizationTool) ImageManager() ImageManager                  { return v.imageManager }
func (v *VirtualizationTool) RawDevices() []string                        { return v.rawDevices }
func (v *VirtualizationTool) KubeletRootDir() string                      { return v.kubeletRootDir }
