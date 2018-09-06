/*
Copyright 2016-2018 Mirantis

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

package manager

import (
	"errors"
	"fmt"
	"time"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
)

const (
	runtimeAPIVersion = "0.1.0"
	runtimeName       = "virtlet"
	runtimeVersion    = "0.1.0"
)

// StreamServer denotes a server that handles Attach and PortForward requests.
type StreamServer interface {
	GetAttach(req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error)
	GetPortForward(req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error)
}

// GCHandler performs GC when a container is deleted.
type GCHandler interface {
	GC() error
}

// VirtletRuntimeService handles CRI runtime service calls.
type VirtletRuntimeService struct {
	virtTool      *libvirttools.VirtualizationTool
	metadataStore metadata.Store
	fdManager     tapmanager.FDManager
	streamServer  StreamServer
	gcHandler     GCHandler
	clock         clockwork.Clock
}

// NewVirtletRuntimeService returns a new instance of VirtletRuntimeService.
func NewVirtletRuntimeService(
	virtTool *libvirttools.VirtualizationTool,
	metadataStore metadata.Store,
	fdManager tapmanager.FDManager,
	streamServer StreamServer,
	gcHandler GCHandler,
	clock clockwork.Clock) *VirtletRuntimeService {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &VirtletRuntimeService{
		virtTool:      virtTool,
		metadataStore: metadataStore,
		fdManager:     fdManager,
		streamServer:  streamServer,
		gcHandler:     gcHandler,
		clock:         clock,
	}
}

// Version implements Version method of CRI.
func (v *VirtletRuntimeService) Version(ctx context.Context, in *kubeapi.VersionRequest) (*kubeapi.VersionResponse, error) {
	vRuntimeAPIVersion := runtimeAPIVersion
	vRuntimeName := runtimeName
	vRuntimeVersion := runtimeVersion
	return &kubeapi.VersionResponse{
		Version:           vRuntimeAPIVersion,
		RuntimeName:       vRuntimeName,
		RuntimeVersion:    vRuntimeVersion,
		RuntimeApiVersion: vRuntimeVersion,
	}, nil
}

//
// Sandboxes
//

// RunPodSandbox implements RunPodSandbox method of CRI.
func (v *VirtletRuntimeService) RunPodSandbox(ctx context.Context, in *kubeapi.RunPodSandboxRequest) (response *kubeapi.RunPodSandboxResponse, retErr error) {
	config := in.GetConfig()
	if config == nil {
		return nil, errors.New("no pod sandbox config passed to RunPodSandbox")
	}
	podName := "<no metadata>"
	if config.Metadata != nil {
		podName = config.Metadata.Name
	}
	if err := validatePodSandboxConfig(config); err != nil {
		return nil, err
	}
	podID := config.Metadata.Uid
	podNs := config.Metadata.Namespace

	// Check if sandbox already exists, it may happen when virtlet restarts and kubelet "thinks" that sandbox disappered
	sandbox := v.metadataStore.PodSandbox(podID)
	sandboxInfo, err := sandbox.Retrieve()
	if err == nil && sandboxInfo != nil {
		if sandboxInfo.State == types.PodSandboxState_SANDBOX_READY {
			return &kubeapi.RunPodSandboxResponse{
				PodSandboxId: podID,
			}, nil
		}
	}

	state := kubeapi.PodSandboxState_SANDBOX_READY
	pnd := &tapmanager.PodNetworkDesc{
		PodID:   podID,
		PodNs:   podNs,
		PodName: podName,
	}
	// Mimic kubelet's method of handling nameservers.
	// As of k8s 1.5.2, kubelet doesn't use any nameserver information from CNI.
	// (TODO: recheck this for 1.6)
	// CNI is used just to configure the network namespace and CNI DNS
	// info is ignored. Instead of this, DnsConfig from PodSandboxConfig
	// is used to configure container's resolv.conf.
	if config.DnsConfig != nil {
		pnd.DNS = &cnitypes.DNS{
			Nameservers: config.DnsConfig.Servers,
			Search:      config.DnsConfig.Searches,
			Options:     config.DnsConfig.Options,
		}
	}

	fdPayload := &tapmanager.GetFDPayload{Description: pnd}
	csnBytes, err := v.fdManager.AddFDs(podID, fdPayload)
	// The reason that defer here is that it is also necessary to ReleaseFDs if AddFDs fail
	// Try to clean up CNI netns (this may be necessary e.g. in case of multiple CNI plugins with CNI Genie)
	defer func() {
		if retErr != nil {
			// Try to clean up CNI netns if we couldn't add the pod to the metadata store or if AddFDs call wasn't
			// successful to avoid leaking resources
			if fdErr := v.fdManager.ReleaseFDs(podID); fdErr != nil {
				glog.Errorf("Error removing pod %s (%s) from CNI network: %v", podName, podID, fdErr)
			}
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("Error adding pod %s (%s) to CNI network: %v", podName, podID, err)
	}

	psi, err := metadata.NewPodSandboxInfo(
		CRIPodSandboxConfigToPodSandboxConfig(config),
		csnBytes, types.PodSandboxState(state), v.clock)
	if err != nil {
		return nil, err
	}

	sandbox = v.metadataStore.PodSandbox(config.Metadata.Uid)
	if err := sandbox.Save(
		func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
			return psi, nil
		},
	); err != nil {
		return nil, err
	}

	return &kubeapi.RunPodSandboxResponse{
		PodSandboxId: podID,
	}, nil
}

// StopPodSandbox implements StopPodSandbox method of CRI.
func (v *VirtletRuntimeService) StopPodSandbox(ctx context.Context, in *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	sandbox := v.metadataStore.PodSandbox(in.PodSandboxId)
	switch sandboxInfo, err := sandbox.Retrieve(); {
	case err != nil:
		return nil, err
	case sandboxInfo == nil:
		return nil, fmt.Errorf("sandbox %q not found in Virtlet metadata store", in.PodSandboxId)
	// check if the sandbox is already stopped
	case sandboxInfo.State != types.PodSandboxState_SANDBOX_NOTREADY:
		if err := sandbox.Save(
			func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
				// make sure the pod is not removed during the call
				if c != nil {
					c.State = types.PodSandboxState_SANDBOX_NOTREADY
				}
				return c, nil
			},
		); err != nil {
			return nil, err
		}

		if err := v.fdManager.ReleaseFDs(in.PodSandboxId); err != nil {
			glog.Errorf("Error releasing tap fd for the pod %q: %v", in.PodSandboxId, err)
		}
	}

	response := &kubeapi.StopPodSandboxResponse{}
	return response, nil
}

// RemovePodSandbox method implements RemovePodSandbox from CRI.
func (v *VirtletRuntimeService) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podSandboxID := in.PodSandboxId

	if err := v.metadataStore.PodSandbox(podSandboxID).Save(
		func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
			return nil, nil
		},
	); err != nil {
		return nil, err
	}

	response := &kubeapi.RemovePodSandboxResponse{}
	return response, nil
}

// PodSandboxStatus method implements PodSandboxStatus from CRI.
func (v *VirtletRuntimeService) PodSandboxStatus(ctx context.Context, in *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	podSandboxID := in.PodSandboxId

	sandbox := v.metadataStore.PodSandbox(podSandboxID)
	sandboxInfo, err := sandbox.Retrieve()
	if err != nil {
		return nil, err
	}
	if sandboxInfo == nil {
		return nil, fmt.Errorf("sandbox %q not found in Virtlet metadata store", podSandboxID)
	}
	status := PodSandboxInfoToCRIPodSandboxStatus(sandboxInfo)

	var cniResult *cnicurrent.Result
	if sandboxInfo.ContainerSideNetwork != nil {
		cniResult = sandboxInfo.ContainerSideNetwork.Result
	}

	ip := cni.GetPodIP(cniResult)
	if ip != "" {
		status.Network = &kubeapi.PodSandboxNetworkStatus{Ip: ip}
	}

	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	return response, nil
}

// ListPodSandbox method implements ListPodSandbox from CRI.
func (v *VirtletRuntimeService) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	filter := CRIPodSandboxFilterToPodSandboxFilter(in.GetFilter())
	sandboxes, err := v.metadataStore.ListPodSandboxes(filter)
	if err != nil {
		return nil, err
	}
	var podSandboxList []*kubeapi.PodSandbox
	for _, sandbox := range sandboxes {
		sandboxInfo, err := sandbox.Retrieve()
		if err != nil {
			glog.Errorf("Error retrieving pod sandbox %q", sandbox.GetID())
		}
		if sandboxInfo != nil {
			podSandboxList = append(podSandboxList, PodSandboxInfoToCRIPodSandbox(sandboxInfo))
		}
	}
	response := &kubeapi.ListPodSandboxResponse{Items: podSandboxList}
	return response, nil
}

//
// Containers
//

// CreateContainer method implements CreateContainer from CRI.
func (v *VirtletRuntimeService) CreateContainer(ctx context.Context, in *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	config := in.GetConfig()
	podSandboxID := in.PodSandboxId
	name := config.GetMetadata().Name

	// Was a container already started in this sandbox?
	// NOTE: there is no distinction between lack of key and other types of
	// errors when accessing boltdb. This will be changed when we switch to
	// storing whole marshaled sandbox metadata as json.
	remainingContainers, err := v.metadataStore.ListPodContainers(podSandboxID)
	if err != nil {
		glog.V(3).Infof("Error retrieving pod %q containers", podSandboxID)
	} else {
		for _, container := range remainingContainers {
			glog.V(3).Infof("CreateContainer: there's already a container in the sandbox (id: %s)", container.GetID())
			response := &kubeapi.CreateContainerResponse{ContainerId: container.GetID()}
			return response, nil
		}
	}

	sandboxInfo, err := v.metadataStore.PodSandbox(podSandboxID).Retrieve()
	if err != nil {
		return nil, err
	}
	if sandboxInfo == nil {
		return nil, fmt.Errorf("sandbox %q not in Virtlet metadata store", podSandboxID)
	}

	fdKey := podSandboxID
	vmConfig, err := GetVMConfig(in, sandboxInfo.ContainerSideNetwork)
	if err != nil {
		return nil, err
	}
	if sandboxInfo.ContainerSideNetwork == nil || sandboxInfo.ContainerSideNetwork.Result == nil {
		fdKey = ""
	}

	uuid, err := v.virtTool.CreateContainer(vmConfig, fdKey)
	if err != nil {
		glog.Errorf("Error creating container %s: %v", name, err)
		return nil, err
	}

	response := &kubeapi.CreateContainerResponse{ContainerId: uuid}
	return response, nil
}

// StartContainer method implements StartContainer from CRI.
func (v *VirtletRuntimeService) StartContainer(ctx context.Context, in *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	info, err := v.virtTool.ContainerInfo(in.ContainerId)
	if err == nil && info != nil && info.State == types.ContainerState_CONTAINER_RUNNING {
		glog.V(2).Infof("StartContainer: Container %s is already running", in.ContainerId)
		response := &kubeapi.StartContainerResponse{}
		return response, nil
	}

	if err := v.virtTool.StartContainer(in.ContainerId); err != nil {
		return nil, err
	}
	response := &kubeapi.StartContainerResponse{}
	return response, nil
}

// StopContainer method implements StopContainer from CRI.
func (v *VirtletRuntimeService) StopContainer(ctx context.Context, in *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	if err := v.virtTool.StopContainer(in.ContainerId, time.Duration(in.Timeout)*time.Second); err != nil {
		return nil, err
	}
	response := &kubeapi.StopContainerResponse{}
	return response, nil
}

// RemoveContainer method implements RemoveContainer from CRI.
func (v *VirtletRuntimeService) RemoveContainer(ctx context.Context, in *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	if err := v.virtTool.RemoveContainer(in.ContainerId); err != nil {
		return nil, err
	}

	if err := v.gcHandler.GC(); err != nil {
		return nil, fmt.Errorf("GC error: %v", err)
	}

	response := &kubeapi.RemoveContainerResponse{}
	return response, nil
}

// ListContainers method implements ListContainers from CRI.
func (v *VirtletRuntimeService) ListContainers(ctx context.Context, in *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	filter := CRIContainerFilterToContainerFilter(in.GetFilter())
	containers, err := v.virtTool.ListContainers(filter)
	if err != nil {
		return nil, err
	}
	var r []*kubeapi.Container
	for _, c := range containers {
		r = append(r, ContainerInfoToCRIContainer(c))
	}
	response := &kubeapi.ListContainersResponse{Containers: r}
	return response, nil
}

// ContainerStatus method implements ContainerStatus from CRI.
func (v *VirtletRuntimeService) ContainerStatus(ctx context.Context, in *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	info, err := v.virtTool.ContainerInfo(in.ContainerId)
	if err != nil {
		return nil, err
	}

	response := &kubeapi.ContainerStatusResponse{Status: ContainerInfoToCRIContainerStatus(info)}
	return response, nil
}

// ExecSync is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) ExecSync(context.Context, *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	return nil, errors.New("not implemented")
}

// Exec is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) Exec(context.Context, *kubeapi.ExecRequest) (*kubeapi.ExecResponse, error) {
	return nil, errors.New("not implemented")
}

// Attach calls streamer server to implement Attach functionality from CRI.
func (v *VirtletRuntimeService) Attach(ctx context.Context, req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	if !req.Stdout && !req.Stderr {
		// Support k8s 1.8 or earlier.
		// We don't care about Stderr because it's not used
		// by the Virtlet stream server.
		req.Stdout = true
	}
	return v.streamServer.GetAttach(req)
}

// PortForward calls streamer server to implement PortForward functionality from CRI.
func (v *VirtletRuntimeService) PortForward(ctx context.Context, req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	return v.streamServer.GetPortForward(req)
}

// UpdateRuntimeConfig is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) UpdateRuntimeConfig(context.Context, *kubeapi.UpdateRuntimeConfigRequest) (*kubeapi.UpdateRuntimeConfigResponse, error) {
	// we don't need to do anything here for now
	return &kubeapi.UpdateRuntimeConfigResponse{}, nil
}

// UpdateContainerResources is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) UpdateContainerResources(context.Context, *kubeapi.UpdateContainerResourcesRequest) (*kubeapi.UpdateContainerResourcesResponse, error) {
	return &kubeapi.UpdateContainerResourcesResponse{}, nil
}

// Status method implements Status from CRI for both types of service, Image and Runtime.
func (v *VirtletRuntimeService) Status(context.Context, *kubeapi.StatusRequest) (*kubeapi.StatusResponse, error) {
	ready := true
	runtimeReadyStr := kubeapi.RuntimeReady
	networkReadyStr := kubeapi.NetworkReady
	return &kubeapi.StatusResponse{
		Status: &kubeapi.RuntimeStatus{
			Conditions: []*kubeapi.RuntimeCondition{
				{
					Type:   runtimeReadyStr,
					Status: ready,
				},
				{
					Type:   networkReadyStr,
					Status: ready,
				},
			},
		},
	}, nil
}

// ContainerStats is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) ContainerStats(ctx context.Context, in *kubeapi.ContainerStatsRequest) (*kubeapi.ContainerStatsResponse, error) {
	return nil, errors.New("ContainerStats() not implemented")
}

// ListContainerStats is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) ListContainerStats(ctx context.Context, in *kubeapi.ListContainerStatsRequest) (*kubeapi.ListContainerStatsResponse, error) {
	return nil, errors.New("ListContainerStats() not implemented")
}

// ReopenContainerLog is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) ReopenContainerLog(ctx context.Context, in *kubeapi.ReopenContainerLogRequest) (*kubeapi.ReopenContainerLogResponse, error) {
	return &kubeapi.ReopenContainerLogResponse{}, nil
}

func validatePodSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	if config.GetMetadata() == nil {
		return errors.New("sandbox config is missing Metadata attribute")
	}

	return nil
}
