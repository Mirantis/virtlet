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
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"k8s.io/apimachinery/pkg/fields"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/network"
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
	ContainerNsUUID       = "67b7fb47-7735-4b64-86d2-6d062d121966"
	defaultKubeletRootDir = "/var/lib/kubelet/pods"
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
}

func (ds *domainSettings) createDomain(config *VMConfig) *libvirtxml.Domain {
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
				{Type: "vnc", Port: -1},
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

		// The following enables nested virtualization.
		// The plan is to enable it via an annotation at some point.
		// It commonly requires kvm_intel module to be loaded like this:
		// modprobe kvm_intel nested=1
		// CPU: &libvirtxml.DomainCPU{
		// 	Mode: "host-model",
		// 	Model: &libvirtxml.DomainCPUModel{
		// 		Fallback: "forbid",
		// 	},
		// 	Features: []libvirtxml.DomainCPUFeature{
		// 		{
		// 			Policy: "require",
		// 			Name:   "vmx",
		// 		},
		// 	},
		// },

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
				{Name: "VIRTLET_POD_NAME", Value: config.PodName},
				{Name: "VIRTLET_POD_NAMESPACE", Value: config.PodNamespace},
				{Name: "VIRTLET_POD_UID", Value: config.PodSandboxID},
				{Name: "VIRTLET_CONTAINER_ID", Value: config.DomainUUID},
				{Name: "VIRTLET_CONTAINER_NAME", Value: config.Name},
				{Name: "CONTAINER_ATTEMPTS", Value: fmt.Sprint(config.Attempt)},
			},
		},
	}

	if os.Getenv("VIRTLET_SRIOV_SUPPORT") != "" {
		domain.QEMUCommandline.Envs = append(domain.QEMUCommandline.Envs,
			libvirtxml.DomainQEMUCommandlineEnv{Name: "VMWRAPPER_KEEP_PRIVS", Value: "1"})
	}
	return domain
}

func canUseKvm() bool {
	if os.Getenv("VIRTLET_DISABLE_KVM") != "" {
		glog.V(0).Infof("VIRTLET_DISABLE_KVM env var not empty, using plain qemu")
		return false
	}
	return true
}

// VirtualizationTool provides methods to operate on libvirt.
type VirtualizationTool struct {
	domainConn     virt.DomainConnection
	volumePool     virt.StoragePool
	imageManager   ImageManager
	metadataStore  metadata.Store
	clock          clockwork.Clock
	forceKVM       bool
	kubeletRootDir string
	rawDevices     []string
	volumeSource   VMVolumeSource
}

var _ volumeOwner = &VirtualizationTool{}

// NewVirtualizationTool verifies existence of volumes pool in libvirt store
// and returns initialized VirtualizationTool.
func NewVirtualizationTool(domainConn virt.DomainConnection, storageConn virt.StorageConnection, imageManager ImageManager,
	metadataStore metadata.Store, volumePoolName, rawDevices string, volumeSource VMVolumeSource) (*VirtualizationTool, error) {

	volumePool, err := ensureStoragePool(storageConn, volumePoolName)
	if err != nil {
		return nil, err
	}
	return &VirtualizationTool{
		domainConn:    domainConn,
		volumePool:    volumePool,
		imageManager:  imageManager,
		metadataStore: metadataStore,
		clock:         clockwork.NewRealClock(),
		// FIXME: kubelet's --root-dir may be something other than /var/lib/kubelet
		// Need to remove it from daemonset mounts (both dev and non-dev)
		// Use 'nsenter -t 1 -m -- tar ...' or something to grab the path
		// from root namespace
		kubeletRootDir: defaultKubeletRootDir,
		rawDevices:     strings.Split(rawDevices, ","),
		volumeSource:   volumeSource,
	}, nil
}

func (v *VirtualizationTool) setForceKVM(forceKVM bool) {
	v.forceKVM = forceKVM
}

func (v *VirtualizationTool) setClock(clock clockwork.Clock) {
	v.clock = clock
}

func (v *VirtualizationTool) setKubeletRootDir(kubeletRootDir string) {
	v.kubeletRootDir = kubeletRootDir
}

func loggingDisabled() bool {
	disabled := os.Getenv("VIRTLET_DISABLE_LOGGING")
	return utils.GetBoolFromString(disabled)
}

func (v *VirtualizationTool) addSerialDevicesToDomain(domain *libvirtxml.Domain) error {
	port := uint(0)
	if !loggingDisabled() {
		domain.Devices.Serials = []libvirtxml.DomainSerial{
			{
				Type:   "unix",
				Source: &libvirtxml.DomainChardevSource{Mode: "connect", Path: "/var/lib/libvirt/streamer.sock"},
				Target: &libvirtxml.DomainSerialTarget{Port: &port},
			},
		}
	} else {
		domain.Devices.Serials = []libvirtxml.DomainSerial{
			{
				Type:   "pty",
				Target: &libvirtxml.DomainSerialTarget{Port: &port},
			},
		}
		domain.Devices.Consoles = []libvirtxml.DomainConsole{
			{
				Type:   "pty",
				Target: &libvirtxml.DomainConsoleTarget{Type: "serial", Port: &port},
			},
		}
	}
	return nil
}

// CreateContainer defines libvirt domain for VM, prepares it's disks and stores
// all info in metadata store.  It returns domain uuid generated basing on pod
// sandbox id.
func (v *VirtualizationTool) CreateContainer(config *VMConfig, netFdKey string) (string, error) {
	if err := config.LoadAnnotations(); err != nil {
		return "", err
	}

	domainUUID := utils.NewUUID5(ContainerNsUUID, config.PodSandboxID)
	// FIXME: this field should be moved to VMStatus struct (to be added)
	config.DomainUUID = domainUUID
	settings := domainSettings{
		domainUUID: domainUUID,
		// Note: using only first 13 characters because libvirt has an issue with handling
		// long path names for qemu monitor socket
		domainName: "virtlet-" + domainUUID[:13] + "-" + config.Name,
		netFdKey:   netFdKey,
	}

	cloneName := "virtlet_root_" + settings.domainUUID
	settings.vcpuNum = config.ParsedAnnotations.VCPUCount
	settings.memory = int(config.MemoryLimitInBytes)
	settings.cpuShares = uint(config.CPUShares)
	settings.cpuPeriod = uint64(config.CPUPeriod)
	// Specified cpu bandwidth limits for domains actually are set equal per each vCPU by libvirt
	// Thus, to limit overall VM's cpu threads consumption by set value in pod definition need to perform division
	settings.cpuQuota = config.CPUQuota / int64(settings.vcpuNum)
	settings.memoryUnit = "b"
	if settings.memory == 0 {
		settings.memory = defaultMemory
		settings.memoryUnit = defaultMemoryUnit
	}

	settings.useKvm = v.forceKVM || canUseKvm()
	domainDef := settings.createDomain(config)

	diskList, err := newDiskList(config, v.volumeSource, v)
	if err != nil {
		return "", err
	}
	domainDef.Devices.Disks, err = diskList.setup()
	if err != nil {
		return "", err
	}

	ok := false
	defer func() {
		if ok {
			return
		}
		if err := v.removeDomain(settings.domainUUID, config, kubeapi.ContainerState_CONTAINER_UNKNOWN, true); err != nil {
			glog.Warningf("Failed to remove domain %q: %v", settings.domainUUID, err)
		}
		if err := diskList.teardown(); err != nil {
			glog.Warningf("error tearing down volumes after an error: %v", err)
		}
	}()

	if err := v.addSerialDevicesToDomain(domainDef); err != nil {
		return "", err
	}

	labels := map[string]string{}
	for k, v := range config.ContainerLabels {
		labels[k] = v
	}
	labels[kubetypes.KubernetesPodNameLabel] = config.PodName
	labels[kubetypes.KubernetesPodNamespaceLabel] = config.PodNamespace
	labels[kubetypes.KubernetesPodUIDLabel] = config.PodSandboxID
	labels[kubetypes.KubernetesContainerNameLabel] = config.Name

	domain, err := v.domainConn.DefineDomain(domainDef)
	if err == nil {
		err = diskList.writeImages(domain)
	}
	if err == nil {
		// FIXME: store VMConfig + VMStatus (to be added)
		err = v.metadataStore.Container(settings.domainUUID).Save(
			func(_ *metadata.ContainerInfo) (*metadata.ContainerInfo, error) {
				return &metadata.ContainerInfo{
					SandboxID:           config.PodSandboxID,
					Name:                config.Name,
					CreatedAt:           v.clock.Now().UnixNano(),
					Image:               config.Image,
					RootImageVolumeName: cloneName,
					Labels:              labels,
					Annotations:         config.ContainerAnnotations,
					Attempt:             config.Attempt,
					State:               kubeapi.ContainerState_CONTAINER_CREATED,
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
		func(c *metadata.ContainerInfo) (*metadata.ContainerInfo, error) {
			// make sure the container is not removed during the call
			if c != nil {
				c.State = kubeapi.ContainerState_CONTAINER_RUNNING
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
			func(c *metadata.ContainerInfo) (*metadata.ContainerInfo, error) {
				// make sure the container is not removed during the call
				if c != nil {
					c.State = kubeapi.ContainerState_CONTAINER_EXITED
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

func (v *VirtualizationTool) getVMConfigFromMetadata(containerID string) (*VMConfig, kubeapi.ContainerState, error) {
	containerInfo, err := v.metadataStore.Container(containerID).Retrieve()
	if err != nil {
		glog.Errorf("Error when retrieving domain %q info from metadata store: %v", containerID, err)
		return nil, kubeapi.ContainerState_CONTAINER_UNKNOWN, err
	}
	if containerInfo == nil {
		// the vm is already removed
		return nil, kubeapi.ContainerState_CONTAINER_UNKNOWN, nil
	}

	podAnnotations := map[string]string{}
	var csn *network.ContainerSideNetwork
	if containerInfo.SandboxID != "" {
		sandbox, err := v.metadataStore.PodSandbox(containerInfo.SandboxID).Retrieve()
		if err != nil {
			glog.Errorf("Error when retrieving sandbox %q info from metadata store: %v", containerInfo.SandboxID, err)
			return nil, kubeapi.ContainerState_CONTAINER_UNKNOWN, nil
		}
		if sandbox == nil {
			glog.Errorf("Missing metadata for sandbox %q set in container %q", containerID, containerInfo.SandboxID)
			return nil, kubeapi.ContainerState_CONTAINER_UNKNOWN, nil
		}
		podAnnotations = sandbox.Annotations
		csn = sandbox.ContainerSideNetwork
	}

	// TODO: here we're using incomplete VMConfig to tear down the volumes
	// What actually needs to be done is storing VMConfig and VMStatus (to be added)
	config := &VMConfig{
		PodSandboxID:         containerInfo.SandboxID,
		Name:                 containerInfo.Name,
		Image:                containerInfo.Image,
		DomainUUID:           containerID,
		PodAnnotations:       podAnnotations,
		ContainerAnnotations: containerInfo.Annotations,
		ContainerLabels:      containerInfo.Labels,
		ContainerSideNetwork: csn,
	}

	if err := config.LoadAnnotations(); err != nil {
		glog.Errorf("Error when parsing annotations for domain %q : %v", containerID, err)
		return nil, containerInfo.State, err
	}

	return config, containerInfo.State, nil
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

	if err != nil {
		glog.Errorf("Volume teardown failed for domain %q: %v", containerID, err)
		return err

	}

	return nil
}

func (v *VirtualizationTool) removeDomain(containerID string, config *VMConfig, state kubeapi.ContainerState, failUponVolumeTeardownFailure bool) error {
	// Give a chance to gracefully stop domain
	// TODO: handle errors - there could be e.g. lost connection error
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil && err != virt.ErrDomainNotFound {
		return err
	}

	if domain != nil {
		if state == kubeapi.ContainerState_CONTAINER_RUNNING {
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

	if err := v.removeDomain(containerID, config, state, state == kubeapi.ContainerState_CONTAINER_CREATED ||
		state == kubeapi.ContainerState_CONTAINER_RUNNING); err != nil {
		return err
	}

	if v.metadataStore.Container(containerID).Save(
		func(_ *metadata.ContainerInfo) (*metadata.ContainerInfo, error) {
			return nil, nil // delete container
		},
	); err != nil {
		glog.Errorf("Error when removing container '%s' from metadata store: %v", containerID, err)
		return err
	}

	return nil
}

func virtToKubeState(domainState virt.DomainState, lastState kubeapi.ContainerState) kubeapi.ContainerState {
	var containerState kubeapi.ContainerState

	switch domainState {
	case virt.DomainStateShutdown:
		// the domain is being shut down, but is still running
		fallthrough
	case virt.DomainStateRunning:
		containerState = kubeapi.ContainerState_CONTAINER_RUNNING
	case virt.DomainStatePaused:
		if lastState == kubeapi.ContainerState_CONTAINER_CREATED {
			containerState = kubeapi.ContainerState_CONTAINER_CREATED
		} else {
			containerState = kubeapi.ContainerState_CONTAINER_EXITED
		}
	case virt.DomainStateShutoff:
		if lastState == kubeapi.ContainerState_CONTAINER_CREATED {
			containerState = kubeapi.ContainerState_CONTAINER_CREATED
		} else {
			containerState = kubeapi.ContainerState_CONTAINER_EXITED
		}
	case virt.DomainStateCrashed:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	case virt.DomainStatePMSuspended:
		containerState = kubeapi.ContainerState_CONTAINER_EXITED
	default:
		containerState = kubeapi.ContainerState_CONTAINER_UNKNOWN
	}

	return containerState
}

func (v *VirtualizationTool) getContainerInfo(domain virt.Domain, containerID string) (*metadata.ContainerInfo, error) {
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
			func(c *metadata.ContainerInfo) (*metadata.ContainerInfo, error) {
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

func (v *VirtualizationTool) getContainer(domain virt.Domain) (*kubeapi.Container, error) {
	containerID, err := domain.UUIDString()
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.getContainerInfo(domain, containerID)
	if err != nil {
		return nil, err
	}

	if containerInfo == nil {
		return nil, nil
	}

	podSandboxID := containerInfo.SandboxID

	metadata := &kubeapi.ContainerMetadata{
		Name:    containerInfo.Name,
		Attempt: containerInfo.Attempt,
	}

	image := &kubeapi.ImageSpec{Image: containerInfo.Image}

	container := &kubeapi.Container{
		Id:           containerID,
		PodSandboxId: podSandboxID,
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

// ListContainers queries libvirt for domains denoted by container id or
// pod standbox id or for all domains and after gathering theirs description
// from metadata and conversion of status from libvirt to kubeapi compatible
// returns them as a list of kubeapi Containers.
func (v *VirtualizationTool) ListContainers(filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
	containers := make([]*kubeapi.Container, 0)

	if filter != nil {
		if filter.Id != "" {
			// Verify if there is container metadata
			containerInfo, err := v.metadataStore.Container(filter.Id).Retrieve()
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
			domainContainers, err := v.metadataStore.ListPodContainers(filter.PodSandboxId)
			if err != nil {
				// There's no such sandbox - looks like it's already removed, so return an empty list
				return containers, nil
			}
			for _, containerMeta := range domainContainers {
				// TODO: Distinguish lack of domain from other errors
				domain, err := v.domainConn.LookupDomainByUUIDString(containerMeta.GetID())
				if err != nil {
					// There's no such domain - looks like it's already removed, so return an empty list
					return containers, nil
				}

				// Verify if there is container metadata
				containerInfo, err := containerMeta.Retrieve()
				if err != nil {
					return nil, err
				}
				if containerInfo == nil {
					// There's no such container - looks like it's already removed, but still is mentioned in sandbox
					return nil, fmt.Errorf("container metadata not found, but it's still mentioned in sandbox %s", filter.PodSandboxId)
				}

				container, err := v.getContainer(domain)
				if err != nil {
					return nil, err
				}
				if filterContainer(container, filter) {
					containers = append(containers, container)
				}
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
			containerID, err := domain.UUIDString()
			if err != nil {
				return nil, err
			}
			glog.V(0).Infof("Failed to find info in bolt for domain with id: %s, so just ignoring as not handled by virtlet.", containerID)
			continue
		}

		if filterContainer(container, filter) {
			containers = append(containers, container)
		}
	}

	return containers, nil
}

// ContainerStatus queries libvirt for domain setatus, converts it to corresponding
// kubeapi container status including container info retrieved from metadata store.
func (v *VirtualizationTool) ContainerStatus(containerID string) (*kubeapi.ContainerStatus, error) {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.getContainerInfo(domain, containerID)
	if err != nil {
		return nil, err
	}

	if containerInfo == nil {
		return nil, fmt.Errorf("missing containerInfo for containerID: %s", containerID)
	}

	image := &kubeapi.ImageSpec{Image: containerInfo.Image}

	return &kubeapi.ContainerStatus{
		Id: containerID,
		Metadata: &kubeapi.ContainerMetadata{
			Name:    containerInfo.Name,
			Attempt: containerInfo.Attempt,
		},
		Image:       image,
		ImageRef:    containerInfo.Image,
		State:       containerInfo.State,
		CreatedAt:   containerInfo.CreatedAt,
		StartedAt:   containerInfo.StartedAt,
		Labels:      containerInfo.Labels,
		Annotations: containerInfo.Annotations,
	}, nil
}

// volumeOwner implementation follows

// StoragePool implements volumeOwner StoragePool method
func (v *VirtualizationTool) StoragePool() virt.StoragePool { return v.volumePool }

// DomainConnection implements volumeOwner DomainConnection method
func (v *VirtualizationTool) DomainConnection() virt.DomainConnection { return v.domainConn }

// ImageManager implements volumeOwner ImageManager method
func (v *VirtualizationTool) ImageManager() ImageManager { return v.imageManager }

// RawDevices implements volumeOwner RawDevices method
func (v *VirtualizationTool) RawDevices() []string { return v.rawDevices }

// KubeletRootDir implements volumeOwner KubeletRootDir method
func (v *VirtualizationTool) KubeletRootDir() string { return v.kubeletRootDir }
