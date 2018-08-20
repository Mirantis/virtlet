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
	"path/filepath"
	"time"

	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"k8s.io/apimachinery/pkg/fields"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
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

	domainStartCheckInterval      = 250 * time.Millisecond
	domainStartTimeout            = 10 * time.Second
	domainShutdownRetryInterval   = 5 * time.Second
	domainShutdownOnRemoveTimeout = 60 * time.Second
	domainDestroyCheckInterval    = 500 * time.Millisecond
	domainDestroyTimeout          = 5 * time.Second

	// ContainerNsUUID template for container ns uuid generation
	ContainerNsUUID = "67b7fb47-7735-4b64-86d2-6d062d121966"

	// KubernetesPodNameLabel is pod name container label (copied from kubetypes).
	KubernetesPodNameLabel = "io.kubernetes.pod.name"
	// KubernetesPodNamespaceLabel is pod namespace container label (copied from kubetypes),
	KubernetesPodNamespaceLabel = "io.kubernetes.pod.namespace"
	// KubernetesPodUIDLabel is uid container label (copied from kubetypes).
	KubernetesPodUIDLabel = "io.kubernetes.pod.uid"
	// KubernetesContainerNameLabel is container name label (copied from kubetypes)
	KubernetesContainerNameLabel = "io.kubernetes.container.name"
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
	netFdKey         string
	enableSriov      bool
	cpuModel         string
}

func (ds *domainSettings) createDomain(config *types.VMConfig) *libvirtxml.Domain {
	domainType := defaultDomainType
	emulator := defaultEmulator
	if !ds.useKvm {
		domainType = noKvmDomainType
		emulator = noKvmEmulator
	}

	scsiControllerIndex := uint(0)
	domain := &libvirtxml.Domain{
		Devices: &libvirtxml.DomainDeviceList{
			Emulator: "/vmwrapper",
			Inputs: []libvirtxml.DomainInput{
				{Type: "tablet", Bus: "usb"},
			},
			Graphics: []libvirtxml.DomainGraphic{
				{VNC: &libvirtxml.DomainGraphicVNC{Port: -1}},
			},
			Videos: []libvirtxml.DomainVideo{
				{Model: libvirtxml.DomainVideoModel{Type: "cirrus"}},
			},
			Controllers: []libvirtxml.DomainController{
				{Type: "scsi", Index: &scsiControllerIndex, Model: "virtio-scsi"},
			},
		},

		OS: &libvirtxml.DomainOS{
			Type: &libvirtxml.DomainOSType{Type: "hvm"},
			BootDevices: []libvirtxml.DomainBootDevice{
				{Dev: "hd"},
			},
		},

		Features: &libvirtxml.DomainFeatureList{ACPI: &libvirtxml.DomainFeature{}},

		OnPoweroff: "destroy",
		OnReboot:   "restart",
		OnCrash:    "restart",

		Type: domainType,

		Name:   ds.domainName,
		UUID:   ds.domainUUID,
		Memory: &libvirtxml.DomainMemory{Value: uint(ds.memory), Unit: ds.memoryUnit},
		VCPU:   &libvirtxml.DomainVCPU{Value: ds.vcpuNum},
		CPUTune: &libvirtxml.DomainCPUTune{
			Shares: &libvirtxml.DomainCPUTuneShares{Value: ds.cpuShares},
			Period: &libvirtxml.DomainCPUTunePeriod{Value: ds.cpuPeriod},
			Quota:  &libvirtxml.DomainCPUTuneQuota{Value: ds.cpuQuota},
		},
		// This causes '"qemu: qemu_thread_create: Resource temporarily unavailable"' QEMU errors
		// when Virtlet is run as a non-privileged user.
		// Under strace, it looks like a bunch of mmap()s failing with EAGAIN
		// which happens due to mlockall() call somewhere above that.
		// This could be worked around using setrlimit() but really
		// swap handling is not needed here because it's incorrect
		// to have swap enabled on the nodes of a real Kubernetes cluster.

		// MemoryBacking: &libvirtxml.DomainMemoryBacking{Locked: &libvirtxml.DomainMemoryBackingLocked{}},

		QEMUCommandline: &libvirtxml.DomainQEMUCommandline{
			Envs: []libvirtxml.DomainQEMUCommandlineEnv{
				{Name: "VIRTLET_EMULATOR", Value: emulator},
				{Name: "VIRTLET_NET_KEY", Value: ds.netFdKey},
				{Name: "VIRTLET_CONTAINER_ID", Value: config.DomainUUID},
				{Name: "VIRTLET_CONTAINER_LOG_PATH", Value: filepath.Join(config.LogDirectory, config.LogPath)},
			},
		},
	}

	// Set cpu model.
	// If user understand the cpu definition of libvirt,
	// the user is very professional, we prior to use it.
	if config.ParsedAnnotations.CPUSetting != nil {
		domain.CPU = config.ParsedAnnotations.CPUSetting
	} else {
		switch ds.cpuModel {
		case types.CPUModelHostModel:
			// The following enables nested virtualization.
			// In case of intel processors it requires nested=1 option
			// for kvm_intel module. That can be passed like this:
			// modprobe kvm_intel nested=1
			domain.CPU = &libvirtxml.DomainCPU{
				Mode: types.CPUModelHostModel,
				Model: &libvirtxml.DomainCPUModel{
					Fallback: "forbid",
				},
				Features: []libvirtxml.DomainCPUFeature{
					{
						Policy: "require",
						Name:   "vmx",
					},
				},
			}
		case "":
			// leave it empty
		default:
			glog.Warningf("Unknown value set in VIRTLET_CPU_MODEL: %q", ds.cpuModel)
		}
	}

	if ds.enableSriov {
		domain.QEMUCommandline.Envs = append(domain.QEMUCommandline.Envs,
			libvirtxml.DomainQEMUCommandlineEnv{Name: "VMWRAPPER_KEEP_PRIVS", Value: "1"})
	}

	return domain
}

// VirtualizationConfig specifies configuration options for VirtualizationTool.
type VirtualizationConfig struct {
	// True if KVM should be disabled
	DisableKVM bool
	// True if SR-IOV support needs to be enabled
	EnableSriov bool
	// List of raw devices that can be accessed by the VM.
	RawDevices []string
	// Kubelet's root dir
	// FIXME: kubelet's --root-dir may be something other than /var/lib/kubelet
	// Need to remove it from daemonset mounts (both dev and non-dev)
	// Use 'nsenter -t 1 -m -- tar ...' or something to grab the path
	// from root namespace
	KubeletRootDir string
	// The path of streamer socket used for
	// logging. By default, the path is empty. When the path is empty,
	// logging is disabled for the VMs.
	StreamerSocketPath string
	// The name of libvirt volume pool to use for the VMs.
	VolumePoolName string
	// CPUModel contains type (can be overloaded by pod annotation)
	// of cpu model to be passed in libvirt domain definition.
	// Empty value denotes libvirt defaults usage.
	CPUModel string
	// Path to the directory used for shared filesystems
	SharedFilesystemPath string
}

// VirtualizationTool provides methods to operate on libvirt.
type VirtualizationTool struct {
	domainConn    virt.DomainConnection
	storageConn   virt.StorageConnection
	imageManager  ImageManager
	metadataStore metadata.Store
	clock         clockwork.Clock
	volumeSource  VMVolumeSource
	config        VirtualizationConfig
	mounter       utils.Mounter
}

var _ volumeOwner = &VirtualizationTool{}

// NewVirtualizationTool verifies existence of volumes pool in libvirt store
// and returns initialized VirtualizationTool.
func NewVirtualizationTool(domainConn virt.DomainConnection,
	storageConn virt.StorageConnection, imageManager ImageManager,
	metadataStore metadata.Store, volumeSource VMVolumeSource,
	config VirtualizationConfig, mounter utils.Mounter) *VirtualizationTool {
	return &VirtualizationTool{
		domainConn:    domainConn,
		storageConn:   storageConn,
		imageManager:  imageManager,
		metadataStore: metadataStore,
		clock:         clockwork.NewRealClock(),
		volumeSource:  volumeSource,
		config:        config,
		mounter:       mounter,
	}
}

// SetClock sets the clock to use (used in tests)
func (v *VirtualizationTool) SetClock(clock clockwork.Clock) {
	v.clock = clock
}

func (v *VirtualizationTool) addSerialDevicesToDomain(domain *libvirtxml.Domain) error {
	port := uint(0)
	timeout := uint(1)
	if v.config.StreamerSocketPath != "" {
		domain.Devices.Serials = []libvirtxml.DomainSerial{
			{
				Source: &libvirtxml.DomainChardevSource{
					UNIX: &libvirtxml.DomainChardevSourceUNIX{
						Mode: "connect",
						Path: v.config.StreamerSocketPath,
						Reconnect: &libvirtxml.DomainChardevSourceReconnect{
							Enabled: "yes",
							Timeout: &timeout,
						},
					},
				},
				Target: &libvirtxml.DomainSerialTarget{Port: &port},
			},
		}
	} else {
		domain.Devices.Serials = []libvirtxml.DomainSerial{
			{
				Target: &libvirtxml.DomainSerialTarget{Port: &port},
			},
		}
		domain.Devices.Consoles = []libvirtxml.DomainConsole{
			{
				Target: &libvirtxml.DomainConsoleTarget{Type: "serial", Port: &port},
			},
		}
	}
	return nil
}

// CreateContainer defines libvirt domain for VM, prepares it's disks and stores
// all info in metadata store.  It returns domain uuid generated basing on pod
// sandbox id.
func (v *VirtualizationTool) CreateContainer(config *types.VMConfig, netFdKey string) (string, error) {
	if err := config.LoadAnnotations(); err != nil {
		return "", err
	}

	domainUUID := utils.NewUUID5(ContainerNsUUID, config.PodSandboxID)
	// FIXME: this field should be moved to VMStatus struct (to be added)
	config.DomainUUID = domainUUID
	cpuModel := v.config.CPUModel
	if config.ParsedAnnotations.CPUModel != "" {
		cpuModel = string(config.ParsedAnnotations.CPUModel)
	}
	settings := domainSettings{
		domainUUID: domainUUID,
		// Note: using only first 13 characters because libvirt has an issue with handling
		// long path names for qemu monitor socket
		domainName:  "virtlet-" + domainUUID[:13] + "-" + config.Name,
		netFdKey:    netFdKey,
		vcpuNum:     config.ParsedAnnotations.VCPUCount,
		memory:      int(config.MemoryLimitInBytes),
		cpuShares:   uint(config.CPUShares),
		cpuPeriod:   uint64(config.CPUPeriod),
		enableSriov: v.config.EnableSriov,
		// CPU bandwidth limits for domains are actually set equal per
		// each vCPU by libvirt. Thus, to limit overall VM's CPU
		// threads consumption by the value from the pod definition
		// we need to perform this division
		cpuQuota:   config.CPUQuota / int64(config.ParsedAnnotations.VCPUCount),
		memoryUnit: "b",
		useKvm:     !v.config.DisableKVM,
		cpuModel:   cpuModel,
	}
	if settings.memory == 0 {
		settings.memory = defaultMemory
		settings.memoryUnit = defaultMemoryUnit
	}

	domainDef := settings.createDomain(config)
	diskList, err := newDiskList(config, v.volumeSource, v)
	if err != nil {
		return "", err
	}
	domainDef.Devices.Disks, domainDef.Devices.Filesystems, err = diskList.setup()
	if err != nil {
		return "", err
	}

	ok := false
	defer func() {
		if ok {
			return
		}
		if err := v.removeDomain(settings.domainUUID, config, types.ContainerState_CONTAINER_UNKNOWN, true); err != nil {
			glog.Warningf("Failed to remove domain %q: %v", settings.domainUUID, err)
		}
		if err := diskList.teardown(); err != nil {
			glog.Warningf("error tearing down volumes after an error: %v", err)
		}
	}()

	if err := v.addSerialDevicesToDomain(domainDef); err != nil {
		return "", err
	}

	if config.ContainerLabels == nil {
		config.ContainerLabels = map[string]string{}
	}
	config.ContainerLabels[kubetypes.KubernetesPodNameLabel] = config.PodName
	config.ContainerLabels[kubetypes.KubernetesPodNamespaceLabel] = config.PodNamespace
	config.ContainerLabels[kubetypes.KubernetesPodUIDLabel] = config.PodSandboxID
	config.ContainerLabels[kubetypes.KubernetesContainerNameLabel] = config.Name

	domain, err := v.domainConn.DefineDomain(domainDef)
	if err == nil {
		err = diskList.writeImages(domain)
	}
	if err == nil {
		err = v.metadataStore.Container(settings.domainUUID).Save(
			func(_ *types.ContainerInfo) (*types.ContainerInfo, error) {
				return &types.ContainerInfo{
					Name:      config.Name,
					CreatedAt: v.clock.Now().UnixNano(),
					Config:    *config,
					State:     types.ContainerState_CONTAINER_CREATED,
				}, nil
			})
	}
	if err != nil {
		return "", err
	}

	ok = true
	return settings.domainUUID, nil
}

func (v *VirtualizationTool) startContainer(containerID string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return fmt.Errorf("failed to look up domain %q: %v", containerID, err)
	}

	state, err := domain.State()
	if err != nil {
		return fmt.Errorf("failed to get state of the domain %q: %v", containerID, err)
	}
	if state != virt.DomainStateShutoff {
		return fmt.Errorf("domain %q: bad state %v upon StartContainer()", containerID, state)
	}

	if err = domain.Create(); err != nil {
		return fmt.Errorf("failed to create domain %q: %v", containerID, err)
	}

	// XXX: maybe we don't really have to wait here but I couldn't
	// find it in libvirt docs.
	if err = utils.WaitLoop(func() (bool, error) {
		state, err := domain.State()
		if err != nil {
			return false, fmt.Errorf("failed to get state of the domain %q: %v", containerID, err)
		}
		switch state {
		case virt.DomainStateRunning:
			return true, nil
		case virt.DomainStateShutdown:
			return false, fmt.Errorf("unexpected shutdown for new domain %q", containerID)
		case virt.DomainStateCrashed:
			return false, fmt.Errorf("domain %q crashed on start", containerID)
		default:
			return false, nil
		}
	}, domainStartCheckInterval, domainStartTimeout, v.clock); err != nil {
		return err
	}

	return v.metadataStore.Container(containerID).Save(
		func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
			// make sure the container is not removed during the call
			if c != nil {
				c.State = types.ContainerState_CONTAINER_RUNNING
				c.StartedAt = v.clock.Now().UnixNano()
			}
			return c, nil
		})
}

// StartContainer calls libvirt to start domain, waits up to 10 seconds for
// DOMAIN_RUNNING state, then updates it's state in metadata store.
// If there was an error it will be returned to caller after an domain removal
// attempt.  If also it had an error - both of them will be combined.
func (v *VirtualizationTool) StartContainer(containerID string) error {
	if err := v.startContainer(containerID); err != nil {
		// FIXME: we do this here because kubelet may attempt new `CreateContainer()`
		// calls for this VM after failed `StartContainer()` without first removing it.
		// Better solution is perhaps moving domain setup logic to `StartContainer()`
		// and cleaning it all up upon failure, but for now we just remove the VM
		// so the next `CreateContainer()` call succeeds.
		if rmErr := v.RemoveContainer(containerID); rmErr != nil {
			return fmt.Errorf("container start error: %v \n+ container removal error: %v", err, rmErr)
		}

		return err
	}

	return nil
}

// StopContainer calls graceful shutdown of domain and if it was non successful
// it calls libvirt to destroy that domain.
// Successful shutdown or destroy of domain is followed by removal of
// VM info from metadata store.
// Succeeded removal of metadata is followed by volumes cleanup.
func (v *VirtualizationTool) StopContainer(containerID string, timeout time.Duration) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return err
	}

	// We try to shut down the VM gracefully first. This may take several attempts
	// because shutdown requests may be ignored e.g. when the VM boots.
	// If this fails, we just destroy the domain (i.e. power off the VM).
	err = utils.WaitLoop(func() (bool, error) {
		_, err := v.domainConn.LookupDomainByUUIDString(containerID)
		if err == virt.ErrDomainNotFound {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to look up the domain %q: %v", containerID, err)
		}

		// domain.Shutdown() may return 'invalid operation' error if domain is already
		// shut down. But checking the state beforehand will not make the situation
		// any simpler because we'll still have a race, thus we need multiple attempts
		domainShutdownErr := domain.Shutdown()

		state, err := domain.State()
		if err != nil {
			return false, fmt.Errorf("failed to get state of the domain %q: %v", containerID, err)
		}

		if state == virt.DomainStateShutoff {
			return true, nil
		}

		if domainShutdownErr != nil {
			// The domain is not in 'DOMAIN_SHUTOFF' state and domain.Shutdown() failed,
			// so we need to return the error that happened during Shutdown()
			return false, fmt.Errorf("failed to shut down domain %q: %v", containerID, err)
		}

		return false, nil
	}, domainShutdownRetryInterval, timeout, v.clock)

	if err != nil {
		glog.Warningf("Failed to shut down VM %q: %v -- trying to destroy the domain", containerID, err)
		// if the domain is destroyed successfully we return no error
		if err = domain.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy the domain: %v", err)
		}
	}

	if err == nil {
		err = v.metadataStore.Container(containerID).Save(
			func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
				// make sure the container is not removed during the call
				if c != nil {
					c.State = types.ContainerState_CONTAINER_EXITED
				}
				return c, nil
			})
	}

	if err == nil {
		// Note: volume cleanup is done right after domain has been stopped
		// due to by the time the ContainerRemove request all flexvolume
		// data is already removed by kubelet's VolumeManager
		return v.cleanupVolumes(containerID)
	}

	return err
}

func (v *VirtualizationTool) getVMConfigFromMetadata(containerID string) (*types.VMConfig, types.ContainerState, error) {
	containerInfo, err := v.metadataStore.Container(containerID).Retrieve()
	if err != nil {
		glog.Errorf("Error when retrieving domain %q info from metadata store: %v", containerID, err)
		return nil, types.ContainerState_CONTAINER_UNKNOWN, err
	}
	if containerInfo == nil {
		// the vm is already removed
		return nil, types.ContainerState_CONTAINER_UNKNOWN, nil
	}

	return &containerInfo.Config, containerInfo.State, nil
}

func (v *VirtualizationTool) cleanupVolumes(containerID string) error {
	config, _, err := v.getVMConfigFromMetadata(containerID)
	if err != nil {
		return err
	}

	if config == nil {
		glog.Warningf("No info found for domain %q in metadata store. Volume cleanup skipped.", containerID)
		return nil
	}

	diskList, err := newDiskList(config, v.volumeSource, v)
	if err == nil {
		err = diskList.teardown()
	}

	var errs []string
	if err != nil {
		glog.Errorf("Volume teardown failed for domain %q: %v", containerID, err)
		errs = append(errs, err.Error())
	}

	return nil
}

func (v *VirtualizationTool) removeDomain(containerID string, config *types.VMConfig, state types.ContainerState, failUponVolumeTeardownFailure bool) error {
	// Give a chance to gracefully stop domain
	// TODO: handle errors - there could be e.g. lost connection error
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil && err != virt.ErrDomainNotFound {
		return err
	}

	if domain != nil {
		if state == types.ContainerState_CONTAINER_RUNNING {
			if err := domain.Destroy(); err != nil {
				return fmt.Errorf("failed to destroy the domain: %v", err)
			}
		}

		if err := domain.Undefine(); err != nil {
			return fmt.Errorf("error undefining the domain %q: %v", containerID, err)
		}

		// Wait until domain is really removed or timeout after 5 sec.
		if err := utils.WaitLoop(func() (bool, error) {
			if _, err := v.domainConn.LookupDomainByUUIDString(containerID); err == virt.ErrDomainNotFound {
				return true, nil
			} else if err != nil {
				// Unexpected error occurred
				return false, fmt.Errorf("error looking up domain %q: %v", containerID, err)
			}
			return false, nil
		}, domainDestroyCheckInterval, domainDestroyTimeout, v.clock); err != nil {
			return err
		}
	}

	diskList, err := newDiskList(config, v.volumeSource, v)
	if err == nil {
		err = diskList.teardown()
	}

	switch {
	case err == nil:
		return nil
	case failUponVolumeTeardownFailure:
		return err
	default:
		glog.Warningf("Error during volume teardown for container %s: %v", containerID, err)
		return nil
	}
}

// RemoveContainer tries to gracefully stop domain, then forcibly removes it
// even if it's still running.
// It waits up to 5 sec for doing the job by libvirt.
func (v *VirtualizationTool) RemoveContainer(containerID string) error {
	config, state, err := v.getVMConfigFromMetadata(containerID)

	if err != nil {
		return err
	}

	if config == nil {
		glog.Warningf("No info found for domain %q in metadata store. Domain cleanup skipped", containerID)
		return nil
	}

	if err := v.removeDomain(containerID, config, state, state == types.ContainerState_CONTAINER_CREATED ||
		state == types.ContainerState_CONTAINER_RUNNING); err != nil {
		return err
	}

	if v.metadataStore.Container(containerID).Save(
		func(_ *types.ContainerInfo) (*types.ContainerInfo, error) {
			return nil, nil // delete container
		},
	); err != nil {
		glog.Errorf("Error when removing container '%s' from metadata store: %v", containerID, err)
		return err
	}

	return nil
}

func virtToKubeState(domainState virt.DomainState, lastState types.ContainerState) types.ContainerState {
	var containerState types.ContainerState

	switch domainState {
	case virt.DomainStateShutdown:
		// the domain is being shut down, but is still running
		fallthrough
	case virt.DomainStateRunning:
		containerState = types.ContainerState_CONTAINER_RUNNING
	case virt.DomainStatePaused:
		if lastState == types.ContainerState_CONTAINER_CREATED {
			containerState = types.ContainerState_CONTAINER_CREATED
		} else {
			containerState = types.ContainerState_CONTAINER_EXITED
		}
	case virt.DomainStateShutoff:
		if lastState == types.ContainerState_CONTAINER_CREATED {
			containerState = types.ContainerState_CONTAINER_CREATED
		} else {
			containerState = types.ContainerState_CONTAINER_EXITED
		}
	case virt.DomainStateCrashed:
		containerState = types.ContainerState_CONTAINER_EXITED
	case virt.DomainStatePMSuspended:
		containerState = types.ContainerState_CONTAINER_EXITED
	default:
		containerState = types.ContainerState_CONTAINER_UNKNOWN
	}

	return containerState
}

func (v *VirtualizationTool) getPodContainer(podSandboxID string) (*types.ContainerInfo, error) {
	// FIXME: is it possible for multiple containers to exist?
	domainContainers, err := v.metadataStore.ListPodContainers(podSandboxID)
	if err != nil {
		// There's no such sandbox. Looks like it's already removed, so return an empty list
		return nil, nil
	}
	for _, containerMeta := range domainContainers {
		// TODO: Distinguish lack of domain from other errors
		_, err := v.domainConn.LookupDomainByUUIDString(containerMeta.GetID())
		if err != nil {
			// There's no such domain. Looks like it's already removed, so return an empty list
			return nil, nil
		}

		// Verify if there is container metadata
		containerInfo, err := containerMeta.Retrieve()
		if err != nil {
			return nil, err
		}
		if containerInfo == nil {
			// There's no such container - looks like it's already removed, but still is mentioned in sandbox
			return nil, fmt.Errorf("container metadata not found, but it's still mentioned in sandbox %s", podSandboxID)
		}

		return containerInfo, nil
	}
	return nil, nil
}

// ListContainers queries libvirt for domains denoted by container id or
// pod standbox id or for all domains and after gathering theirs description
// from metadata and conversion of status from libvirt to kubeapi compatible
// returns them as a list of kubeapi Containers.
func (v *VirtualizationTool) ListContainers(filter *types.ContainerFilter) ([]*types.ContainerInfo, error) {
	var containers []*types.ContainerInfo
	switch {
	case filter != nil && filter.Id != "":
		containerInfo, err := v.ContainerInfo(filter.Id)
		if err != nil || containerInfo == nil {
			return nil, err
		}
		containers = append(containers, containerInfo)
	case filter != nil && filter.PodSandboxID != "":
		containerInfo, err := v.getPodContainer(filter.PodSandboxID)
		if err != nil || containerInfo == nil {
			return nil, err
		}
		containers = append(containers, containerInfo)
	default:
		// Get list of all the defined domains from libvirt
		// and check each container against the remaining
		// filter settings
		domains, err := v.domainConn.ListDomains()
		if err != nil {
			return nil, err
		}
		for _, domain := range domains {
			containerID, err := domain.UUIDString()
			if err != nil {
				return nil, err
			}
			containerInfo, err := v.ContainerInfo(containerID)
			if err != nil {
				return nil, err
			}

			if containerInfo == nil {
				glog.V(1).Infof("Failed to find info for domain with id %q in virtlet db, considering it a non-virtlet libvirt domain.", containerID)
				continue
			}
			containers = append(containers, containerInfo)
		}
	}

	if filter == nil {
		return containers, nil
	}

	var r []*types.ContainerInfo
	for _, c := range containers {
		if filterContainer(c, *filter) {
			r = append(r, c)
		}
	}

	return r, nil
}

// ContainerInfo returns info for the specified container, making sure it's also
// present among libvirt domains. If it isn't, the function returns nil
func (v *VirtualizationTool) ContainerInfo(containerID string) (*types.ContainerInfo, error) {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.metadataStore.Container(containerID).Retrieve()
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
		if err := v.metadataStore.Container(containerID).Save(
			func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
				// make sure the container is not removed during the call
				if c != nil {
					c.State = containerState
				}
				return c, nil
			},
		); err != nil {
			return nil, err
		}
		containerInfo.State = containerState
	}
	return containerInfo, nil
}

// VMStats returns current cpu/memory/disk usage for VM
func (v *VirtualizationTool) VMStats(containerID string) (*types.VMStats, error) {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return nil, err
	}
	vs := types.VMStats{
		Timestamp:   time.Now().UnixNano(),
		ContainerID: containerID,
	}
	if rss, err := domain.GetRSS(); err != nil {
		return nil, err
	} else {
		vs.MemoryUsage = rss
	}
	if cpuTime, err := domain.GetCPUTime(); err != nil {
		return nil, err
	} else {
		vs.CpuUsage = cpuTime
	}
	// TODO: find image created by rootfs provider, stat it and fill there
	// used by it bytes/inodes. Additionally mountpoint of fs on which
	// root volume is located should be found and filled there
	return &vs, nil
}

// ListVMStats returns statistics (same as VMStats) for all containers matching
// provided filter (id AND podstandboxid AND labels)
func (v *VirtualizationTool) ListVMStats(filter *types.VMStatsFilter) ([]types.VMStats, error) {
	var containersFilter *types.ContainerFilter
	if filter != nil {
		containersFilter = &types.ContainerFilter{}
		if filter.Id != "" {
			containersFilter.Id = filter.Id
		}
		if filter.PodSandboxID != "" {
			containersFilter.PodSandboxID = filter.PodSandboxID
		}
		if filter.LabelSelector != nil {
			containersFilter.LabelSelector = filter.LabelSelector
		}
	}

	infos, err := v.ListContainers(containersFilter)
	if err != nil {
		return nil, err
	}

	var statsList []types.VMStats
	for _, info := range infos {
		if stats, err := v.VMStats(info.Id); err != nil {
			return nil, err
		} else {
			statsList = append(statsList, *stats)
		}
	}
	return statsList, nil
}

// volumeOwner implementation follows

// StoragePool returns StoragePool for volumes
func (v *VirtualizationTool) StoragePool() (virt.StoragePool, error) {
	return ensureStoragePool(v.storageConn, v.config.VolumePoolName)
}

// DomainConnection implements volumeOwner DomainConnection method
func (v *VirtualizationTool) DomainConnection() virt.DomainConnection { return v.domainConn }

// ImageManager implements volumeOwner ImageManager method
func (v *VirtualizationTool) ImageManager() ImageManager { return v.imageManager }

// RawDevices implements volumeOwner RawDevices method
func (v *VirtualizationTool) RawDevices() []string { return v.config.RawDevices }

// KubeletRootDir implements volumeOwner KubeletRootDir method
func (v *VirtualizationTool) KubeletRootDir() string { return v.config.KubeletRootDir }

// VolumePoolName implements volumeOwner VolumePoolName method
func (v *VirtualizationTool) VolumePoolName() string { return v.config.VolumePoolName }

// Mounter implements volumeOwner Mounter method
func (v *VirtualizationTool) Mounter() utils.Mounter { return v.mounter }

// SharedFilesystemPath implements volumeOwner SharedFilesystemPath method
func (v *VirtualizationTool) SharedFilesystemPath() string { return v.config.SharedFilesystemPath }

func filterContainer(containerInfo *types.ContainerInfo, filter types.ContainerFilter) bool {
	if filter.Id != "" && containerInfo.Id != filter.Id {
		return false
	}

	if filter.PodSandboxID != "" && containerInfo.Config.PodSandboxID != filter.PodSandboxID {
		return false
	}

	if filter.State != nil && containerInfo.State != *filter.State {
		return false
	}
	if filter.LabelSelector != nil {
		sel := fields.SelectorFromSet(filter.LabelSelector)
		if !sel.Matches(fields.Set(containerInfo.Config.ContainerLabels)) {
			return false
		}
	}

	return true
}
