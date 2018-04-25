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

package manager

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/image"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/stream"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
)

const (
	runtimeAPIVersion       = "0.1.0"
	runtimeName             = "virtlet"
	runtimeVersion          = "0.1.0"
	defaultDownloadProtocol = "https"
)

// VirtletManager serves grpc CRI requests translating them to libvirt calls
// using additional data stored in metadata store. Network part also uses
// tapmanager to prepare and cleanup network environment for VM.
type VirtletManager struct {
	server *grpc.Server
	// libvirt
	imageStore                image.Store
	libvirtVirtualizationTool *libvirttools.VirtualizationTool
	// metadata
	metadataStore   metadata.Store
	fdManager       tapmanager.FDManager
	imageTranslator image.Translator
	StreamServer    *stream.Server
	clock           clockwork.Clock
}

// NewVirtletManager prepares libvirt connection, volumes component,
// using them to prepare virtualization tool.  It calls garbage collection
// for virtualization tool and image store, then it registers newly prepared
// VirtletManager instance as runtime and image service through a grpc server.
func NewVirtletManager(virtTool *libvirttools.VirtualizationTool, imageStore image.Store, metadataStore metadata.Store, fdManager tapmanager.FDManager, imageTranslator image.Translator) *VirtletManager {
	return &VirtletManager{
		server:                    grpc.NewServer(),
		imageStore:                imageStore,
		libvirtVirtualizationTool: virtTool,
		metadataStore:             metadataStore,
		fdManager:                 fdManager,
		imageTranslator:           imageTranslator,
		clock:                     clockwork.NewRealClock(),
	}
}

// Register registers VirtletManager with gRPC
func (v *VirtletManager) Register() {
	kubeapi.RegisterRuntimeServiceServer(v.server, v)
	kubeapi.RegisterImageServiceServer(v.server, v)
}

// RecoverAndGC performs the initial actions during VirtletManager
// startup, including recovering network namespaces and performing
// garbage collection for both libvirt and the image store.
func (v *VirtletManager) RecoverAndGC() error {
	var errors []string
	for _, err := range recoverNetworkNamespaces(v.metadataStore, v.fdManager) {
		errors = append(errors, fmt.Sprintf("* error recovering VM network namespaces: %v", err))
	}

	for _, err := range v.libvirtVirtualizationTool.GarbageCollect() {
		errors = append(errors, fmt.Sprintf("* error performing libvirt GC: %v", err))
	}

	if err := v.imageStore.GC(); err != nil {
		errors = append(errors, fmt.Sprintf("* error during image GC: %v", err))
	}

	if len(errors) == 0 {
		return nil
	}

	return fmt.Errorf("errors encountered during recover / GC:\n%s", strings.Join(errors, "\n"))
}

// Serve prepares a listener on unix socket, than it passes that listener to
// main loop of grpc server which handles CRI calls.
func (v *VirtletManager) Serve(addr string) error {
	if err := syscall.Unlink(addr); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	return v.server.Serve(ln)
}

// Stop halts the manager.
func (v *VirtletManager) Stop() {
	v.server.Stop()
}

// Version implements Version method of CRI.
func (v *VirtletManager) Version(ctx context.Context, in *kubeapi.VersionRequest) (*kubeapi.VersionResponse, error) {
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
func (v *VirtletManager) RunPodSandbox(ctx context.Context, in *kubeapi.RunPodSandboxRequest) (*kubeapi.RunPodSandboxResponse, error) {
	config := in.GetConfig()
	if config == nil {
		glog.Errorf("No pod sandbox config passed to RunPodSandbox")
		return nil, errors.New("no pod sandbox config passed to RunPodSandbox")
	}
	podName := "<no metadata>"
	if config.Metadata != nil {
		podName = config.Metadata.Name
	}
	if err := validatePodSandboxConfig(config); err != nil {
		glog.Errorf("Invalid pod config while creating pod sandbox for pod %s: %v", podName, err)
		return nil, err
	}
	podID := config.Metadata.Uid
	podNs := config.Metadata.Namespace

	glog.V(2).Infof("RunPodSandbox called for pod %s (%s)", podName, podID)

	// Check if sandbox already exists, it may happen when virtlet restarts and kubelet "thinks" that sandbox disappered
	sandbox := v.metadataStore.PodSandbox(podID)
	sandboxInfo, err := sandbox.Retrieve()
	if err == nil && sandboxInfo != nil {
		status := sandboxInfo.AsPodSandboxStatus()
		if status.State == kubeapi.PodSandboxState_SANDBOX_READY {
			return &kubeapi.RunPodSandboxResponse{
				PodSandboxId: podID,
			}, err
		}
	}

	glog.V(3).Infof("RunPodSandbox: %s", spew.Sdump(in))
	glog.V(2).Infof("Sandbox config annotations: %v", config.GetAnnotations())

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
	if err != nil {
		// this will cause kubelet to delete the pod sandbox and then retry
		// its creation
		state = kubeapi.PodSandboxState_SANDBOX_NOTREADY
		glog.Errorf("Error when adding pod %s (%s) to CNI network: %v", podName, podID, err)
	}

	psi, err := metadata.NewPodSandboxInfo(config, csnBytes, state, v.clock)
	if err != nil {
		glog.Errorf("Error serializing pod %q (%q) sandbox configuration: %v", podName, podID, err)
		return nil, err
	}

	sandbox = v.metadataStore.PodSandbox(config.Metadata.Uid)
	if storeErr := sandbox.Save(
		func(c *metadata.PodSandboxInfo) (*metadata.PodSandboxInfo, error) {
			return psi, nil
		},
	); storeErr != nil {
		glog.Errorf("Error when creating pod sandbox for pod %s (%s): %v", podName, podID, storeErr)
		return nil, storeErr
	}

	// If we don't return PodSandboxId upon RunPodSandbox, kubelet will not retry
	// RunPodSandbox for this pod after CNI failure
	return &kubeapi.RunPodSandboxResponse{
		PodSandboxId: podID,
	}, err
}

func validatePodSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	metadata := config.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("sandbox config is missing Metadata attribute: %s", spew.Sdump(config))
	}

	return nil
}

// StopPodSandbox implements StopPodSandbox method of CRI.
func (v *VirtletManager) StopPodSandbox(ctx context.Context, in *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	glog.V(2).Infof("StopPodSandbox called for pod %s", in.PodSandboxId)
	glog.V(3).Infof("StopPodSandbox: %s", spew.Sdump(in))
	sandbox := v.metadataStore.PodSandbox(in.PodSandboxId)
	sandboxInfo, err := sandbox.Retrieve()
	if err != nil {
		glog.Errorf("Error retrieving pod sandbox status for pod %q while stopping it: %v", in.PodSandboxId, err)
		return nil, err
	}
	if sandboxInfo == nil {
		glog.Errorf("Sandbox %q doesn't exist", in.PodSandboxId)
		return nil, fmt.Errorf("sandbox %q not found in Virtlet metadata store", in.PodSandboxId)
	}
	status := sandboxInfo.AsPodSandboxStatus()

	// check if the sandbox is already stopped
	if status.State != kubeapi.PodSandboxState_SANDBOX_NOTREADY {
		if err := sandbox.Save(
			func(c *metadata.PodSandboxInfo) (*metadata.PodSandboxInfo, error) {
				// make sure the pod is not removed during the call
				if c != nil {
					c.State = kubeapi.PodSandboxState_SANDBOX_NOTREADY
				}
				return c, nil
			},
		); err != nil {
			glog.Errorf("Error updating pod sandbox status for pod %q while stopping it: %v", in.PodSandboxId, err)
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
func (v *VirtletManager) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podSandboxID := in.PodSandboxId
	glog.V(2).Infof("RemovePodSandbox called for pod %s", podSandboxID)
	glog.V(3).Infof("RemovePodSandbox: %s", spew.Sdump(in))

	if err := v.metadataStore.PodSandbox(podSandboxID).Save(
		func(c *metadata.PodSandboxInfo) (*metadata.PodSandboxInfo, error) {
			return nil, nil
		},
	); err != nil {
		glog.Errorf("Error when removing pod sandbox %q: %v", podSandboxID, err)
		return nil, err
	}

	response := &kubeapi.RemovePodSandboxResponse{}
	glog.V(3).Infof("RemovePodSandbox response: %s", spew.Sdump(response))
	return response, nil
}

// PodSandboxStatus method implements PodSandboxStatus from CRI.
func (v *VirtletManager) PodSandboxStatus(ctx context.Context, in *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	glog.V(3).Infof("PodSandboxStatusStatus: %s", spew.Sdump(in))
	podSandboxID := in.PodSandboxId

	sandbox := v.metadataStore.PodSandbox(podSandboxID)
	sandboxInfo, err := sandbox.Retrieve()
	if err != nil {
		glog.Errorf("Error when getting pod sandbox '%s': %v", podSandboxID, err)
		return nil, err
	}
	if sandboxInfo == nil {
		glog.Errorf("Sandbox %q doesn't exist", podSandboxID)
		return nil, fmt.Errorf("sandbox %q not found in Virtlet metadata store", podSandboxID)
	}
	status := sandboxInfo.AsPodSandboxStatus()

	var cniResult *cnicurrent.Result
	if sandboxInfo.ContainerSideNetwork != nil {
		cniResult = sandboxInfo.ContainerSideNetwork.Result
	}

	ip := cni.GetPodIP(cniResult)
	if ip != "" {
		status.Network = &kubeapi.PodSandboxNetworkStatus{Ip: ip}
	}

	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	glog.V(3).Infof("PodSandboxStatus response: %s", spew.Sdump(response))
	return response, nil
}

// ListPodSandbox method implements ListPodSandbox from CRI.
func (v *VirtletManager) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	filter := in.GetFilter()
	glog.V(4).Infof("Listing sandboxes with filter: %s", spew.Sdump(filter))
	sandboxes, err := v.metadataStore.ListPodSandboxes(filter)
	if err != nil {
		glog.Errorf("Error when listing (with filter: %s) pod sandboxes: %v", spew.Sdump(filter), err)
		return nil, err
	}
	var podSandboxList []*kubeapi.PodSandbox
	for _, sandbox := range sandboxes {
		sandboxInfo, err := sandbox.Retrieve()
		if err != nil {
			glog.Errorf("Error retrieving pod sandbox %q", sandbox.GetID())
		}
		if sandboxInfo != nil {
			podSandboxList = append(podSandboxList, sandboxInfo.AsPodSandbox())
		}
	}
	response := &kubeapi.ListPodSandboxResponse{Items: podSandboxList}
	glog.V(4).Infof("ListPodSandbox response: %s", spew.Sdump(response))
	return response, nil
}

//
// Containers
//

// CreateContainer method implements CreateContainer from CRI.
func (v *VirtletManager) CreateContainer(ctx context.Context, in *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	config := in.GetConfig()
	podSandboxID := in.PodSandboxId
	name := config.GetMetadata().Name

	glog.V(2).Infof("CreateContainer called for name: %s", name)
	glog.V(3).Infof("CreateContainer: %s", spew.Sdump(in))
	glog.V(3).Infof("CreateContainer config: %s", spew.Sdump(config))

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
		glog.Errorf("Error when retrieving pod network configuration for sandbox '%s': %v", podSandboxID, err)
		return nil, err
	}
	if sandboxInfo == nil {
		glog.Errorf("Sandbox %q doesn't exist", podSandboxID)
		return nil, fmt.Errorf("sandbox %q not in Virtlet metadata store", podSandboxID)
	}

	fdKey := podSandboxID
	vmConfig, err := libvirttools.GetVMConfig(in, sandboxInfo.ContainerSideNetwork)
	if err != nil {
		glog.Errorf("Error getting vm config for container %s: %v", name, err)
		return nil, err
	}
	if sandboxInfo.ContainerSideNetwork == nil || sandboxInfo.ContainerSideNetwork.Result == nil {
		fdKey = ""
	}

	uuid, err := v.libvirtVirtualizationTool.CreateContainer(vmConfig, fdKey)
	if err != nil {
		glog.Errorf("Error creating container %s: %v", name, err)
		return nil, err
	}

	response := &kubeapi.CreateContainerResponse{ContainerId: uuid}
	glog.V(3).Infof("CreateContainer response: %s", spew.Sdump(response))
	return response, nil
}

// StartContainer method implements StartContainer from CRI.
func (v *VirtletManager) StartContainer(ctx context.Context, in *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	glog.V(2).Infof("StartContainer called for containerID: %s", in.ContainerId)
	glog.V(3).Infof("StartContainer: %s", spew.Sdump(in))

	status, err := v.libvirtVirtualizationTool.ContainerStatus(in.ContainerId)
	if err == nil && status != nil && status.State == kubeapi.ContainerState_CONTAINER_RUNNING {
		glog.V(2).Infof("StartContainer: Container %s is already running", in.ContainerId)
		response := &kubeapi.StartContainerResponse{}
		return response, nil
	}

	if err := v.libvirtVirtualizationTool.StartContainer(in.ContainerId); err != nil {
		glog.Errorf("Error when starting container %s: %v", in.ContainerId, err)
		return nil, err
	}
	response := &kubeapi.StartContainerResponse{}
	return response, nil
}

// StopContainer method implements StopContainer from CRI.
func (v *VirtletManager) StopContainer(ctx context.Context, in *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	glog.V(2).Infof("StopContainer called for containerID: %s", in.ContainerId)
	glog.V(3).Infof("StopContainer: %s", spew.Sdump(in))

	if err := v.libvirtVirtualizationTool.StopContainer(in.ContainerId, time.Duration(in.Timeout)*time.Second); err != nil {
		glog.Errorf("Error when stopping container %s: %v", in.ContainerId, err)
		return nil, err
	}
	response := &kubeapi.StopContainerResponse{}
	glog.V(2).Infof("Sending stop response for containerID: %s", in.ContainerId)
	return response, nil
}

// RemoveContainer method implements RemoveContainer from CRI.
func (v *VirtletManager) RemoveContainer(ctx context.Context, in *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	glog.V(2).Infof("RemoveContainer called for containerID: %s", in.ContainerId)
	glog.V(3).Infof("RemoveContainer: %s", spew.Sdump(in))

	if err := v.libvirtVirtualizationTool.RemoveContainer(in.ContainerId); err != nil {
		glog.Errorf("Error when removing container %q: %v", in.ContainerId, err)
		return nil, err
	}

	if err := v.imageStore.GC(); err != nil {
		glog.Errorf("Error doing image GC after removing container %q: %v", in.ContainerId, err)
		return nil, err
	}

	response := &kubeapi.RemoveContainerResponse{}
	return response, nil
}

// ListContainers method implements ListContainers from CRI.
func (v *VirtletManager) ListContainers(ctx context.Context, in *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	filter := in.GetFilter()
	glog.V(4).Infof("Listing containers with filter: %s", spew.Sdump(filter))
	glog.V(4).Infof("ListContainers: %s", spew.Sdump(in))
	containers, err := v.libvirtVirtualizationTool.ListContainers(filter)
	if err != nil {
		glog.Errorf("Error when listing containers with filter %s: %v", spew.Sdump(filter), err)
		return nil, err
	}
	response := &kubeapi.ListContainersResponse{Containers: containers}
	glog.V(4).Infof("ListContainers response:\n%s\n", spew.Sdump(response))
	return response, nil
}

// ContainerStatus method implements ContainerStatus from CRI.
func (v *VirtletManager) ContainerStatus(ctx context.Context, in *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	glog.V(4).Infof("ContainerStatus: %s", spew.Sdump(in))
	status, err := v.libvirtVirtualizationTool.ContainerStatus(in.ContainerId)
	if err != nil {
		glog.Errorf("Error when getting container '%s' status: %v", in.ContainerId, err)
		return nil, err
	}

	response := &kubeapi.ContainerStatusResponse{Status: status}
	glog.V(4).Infof("ContainerStatus response: %s", spew.Sdump(response))
	return response, nil
}

// ExecSync is a placeholder for an unimplemented CRI method.
func (v *VirtletManager) ExecSync(context.Context, *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	glog.Errorf("ExecSync() not implemented")
	return nil, errors.New("not implemented")
}

// Exec is a placeholder for an unimplemented CRI method.
func (v *VirtletManager) Exec(context.Context, *kubeapi.ExecRequest) (*kubeapi.ExecResponse, error) {
	glog.Errorf("Exec() not implemented")
	return nil, errors.New("not implemented")
}

// Attach calls streamer server to implement Attach functionality from CRI.
func (v *VirtletManager) Attach(ctx context.Context, req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	glog.V(3).Infof("Attach called: %s", spew.Sdump(req))
	if !req.Stdout && !req.Stderr {
		// Support k8s 1.8 or earlier.
		// We don't care about Stderr because it's not used
		// by the Virtlet stream server.
		req.Stdout = true
	}
	return v.StreamServer.GetAttach(req)
}

// PortForward calls streamer server to implement PortForward functionality from CRI.
func (v *VirtletManager) PortForward(ctx context.Context, req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	glog.Errorf("PortForward() not implemented")
	return v.StreamServer.GetPortForward(req)
}

// UpdateRuntimeConfig is a placeholder for an unimplemented CRI method.
func (v *VirtletManager) UpdateRuntimeConfig(context.Context, *kubeapi.UpdateRuntimeConfigRequest) (*kubeapi.UpdateRuntimeConfigResponse, error) {
	// we don't need to do anything here for now
	return &kubeapi.UpdateRuntimeConfigResponse{}, nil
}

// UpdateContainerResources is a placeholder for an unimplemented CRI method.
func (v *VirtletManager) UpdateContainerResources(context.Context, *kubeapi.UpdateContainerResourcesRequest) (*kubeapi.UpdateContainerResourcesResponse, error) {
	return &kubeapi.UpdateContainerResourcesResponse{}, nil
}

// Status method implements Status from CRI for both types of service, Image and Runtime.
func (v *VirtletManager) Status(context.Context, *kubeapi.StatusRequest) (*kubeapi.StatusResponse, error) {
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
func (v *VirtletManager) ContainerStats(ctx context.Context, in *kubeapi.ContainerStatsRequest) (*kubeapi.ContainerStatsResponse, error) {
	glog.V(2).Infof("ContainerStats: %s", spew.Sdump(in))
	return nil, errors.New("ContainerStats() not implemented")
}

// ListContainerStats is a placeholder for an unimplemented CRI method.
func (v *VirtletManager) ListContainerStats(ctx context.Context, in *kubeapi.ListContainerStatsRequest) (*kubeapi.ListContainerStatsResponse, error) {
	glog.V(2).Infof("ListContainerStats: %s", spew.Sdump(in))
	return nil, errors.New("ListContainerStats() not implemented")
}

//
// Images
//

// ListImages method implements ListImages from CRI.
func (v *VirtletManager) ListImages(ctx context.Context, in *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	images, err := v.imageStore.ListImages(in.GetFilter().GetImage().GetImage())
	if err != nil {
		glog.Errorf("ListImages: ERROR: %v", err)
		return nil, err
	}

	response := &kubeapi.ListImagesResponse{Images: make([]*kubeapi.Image, len(images))}
	for n, image := range images {
		response.Images[n] = imageToKubeapi(image)
	}

	glog.V(4).Infof("ListImages response: %s", spew.Sdump(response))
	return response, err
}

// ImageStatus method implements ImageStatus from CRI.
func (v *VirtletManager) ImageStatus(ctx context.Context, in *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	img, err := v.imageStore.ImageStatus(in.GetImage().GetImage())
	if err != nil {
		glog.Errorf("ImageStatus: ERROR: %v", err)
		return nil, err
	}
	response := &kubeapi.ImageStatusResponse{Image: imageToKubeapi(img)}
	glog.V(3).Infof("ImageStatus response: %s", spew.Sdump(response))
	return response, err
}

// PullImage method implements PullImage from CRI.
func (v *VirtletManager) PullImage(ctx context.Context, in *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	imageName := in.GetImage().GetImage()
	glog.V(2).Infof("PullImage called for: %s", imageName)

	ref, err := v.imageStore.PullImage(ctx, imageName, v.imageTranslator)
	if err != nil {
		glog.Errorf("PullImage: ERROR: %v", err)
		return nil, err
	}

	response := &kubeapi.PullImageResponse{ImageRef: ref}
	glog.V(3).Infof("PullImage response: %s", spew.Sdump(response))
	return response, nil
}

// RemoveImage method implements RemoveImage from CRI.
func (v *VirtletManager) RemoveImage(ctx context.Context, in *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	imageName := in.GetImage().GetImage()
	glog.V(2).Infof("RemoveImage called for: %s", imageName)
	if err := v.imageStore.RemoveImage(imageName); err != nil {
		glog.Errorf("RemoveImage: ERROR: %v", err)
		return nil, err
	}
	return &kubeapi.RemoveImageResponse{}, nil
}

// ImageFsInfo is a placeholder an unimplemented CRI method.
func (v *VirtletManager) ImageFsInfo(ctx context.Context, in *kubeapi.ImageFsInfoRequest) (*kubeapi.ImageFsInfoResponse, error) {
	glog.V(2).Infof("ImageFsInfo: %s", spew.Sdump(in))
	return nil, errors.New("ImageFsInfo() not implemented")
}

func imageToKubeapi(img *image.Image) *kubeapi.Image {
	if img == nil {
		return nil
	}
	return &kubeapi.Image{
		Id:       img.Digest,
		RepoTags: []string{img.Name},
		Size_:    img.Size,
	}
}
