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
	"syscall"
	"time"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/imagetranslation"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/stream"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
)

const (
	runtimeAPIVersion       = "0.1.0"
	runtimeName             = "virtlet"
	runtimeVersion          = "0.1.0"
	defaultDownloadProtocol = "https"
)

type VirtletManager struct {
	server *grpc.Server
	// libvirt
	libvirtImageTool          *libvirttools.ImageTool
	libvirtVirtualizationTool *libvirttools.VirtualizationTool
	// metadata
	metadataStore              metadata.MetadataStore
	fdManager                  tapmanager.FDManager
	imageTranslationConfigsDir string
	StreamServer               *stream.Server
}

func NewVirtletManager(libvirtUri, poolName, downloadProtocol, storageBackend, rawDevices, imageTranslationConfigsDir string, metadataStore metadata.MetadataStore, fdManager tapmanager.FDManager) (*VirtletManager, error) {
	err := imagetranslation.RegisterCustomResourceType()
	if err != nil {
		return nil, err
	}

	if downloadProtocol == "" {
		downloadProtocol = defaultDownloadProtocol
	}
	downloader := utils.NewDownloader(downloadProtocol)

	conn, err := libvirttools.NewConnection(libvirtUri)
	if err != nil {
		return nil, err
	}

	libvirtImageTool, err := libvirttools.NewImageTool(conn, downloader, poolName)
	if err != nil {
		return nil, err
	}

	// TODO: there should be easy-to-use VirtualizationTool (or at least VMVolumeSource) provider
	volSrc := libvirttools.CombineVMVolumeSources(
		libvirttools.GetRootVolume,
		libvirttools.ScanFlexvolumes,
		// XXX: GetNocloudVolume must go last because it
		// doesn't produce correct name for cdrom devices
		libvirttools.GetNocloudVolume)
	// TODO: pool name should be passed like for imageTool
	libvirtVirtualizationTool, err := libvirttools.NewVirtualizationTool(conn, conn, libvirtImageTool, metadataStore, "volumes", rawDevices, volSrc)
	if err != nil {
		return nil, err
	}

	if errors := libvirtVirtualizationTool.RecoverNetworkNamespaces(fdManager); errors != nil {
		glog.Warning("The following errors were encountered while recovering the VM network namespaces:")
		for _, err := range errors {
			glog.Warningf("* %q", err)
		}
	}

	if errors := libvirtVirtualizationTool.GarbageCollect(); errors != nil {
		glog.Warning("The following errors were encountered while garbage collection process:")
		for _, err := range errors {
			glog.Warningf("* %q", err)
		}
	}

	virtletManager := &VirtletManager{
		server:                     grpc.NewServer(),
		libvirtImageTool:           libvirtImageTool,
		libvirtVirtualizationTool:  libvirtVirtualizationTool,
		metadataStore:              metadataStore,
		fdManager:                  fdManager,
		imageTranslationConfigsDir: imageTranslationConfigsDir,
	}

	kubeapi.RegisterRuntimeServiceServer(virtletManager.server, virtletManager)
	kubeapi.RegisterImageServiceServer(virtletManager.server, virtletManager)

	return virtletManager, nil
}

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

func (v *VirtletManager) Stop() {
	v.server.Stop()
}

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
	podId := config.Metadata.Uid
	podNs := config.Metadata.Namespace

	glog.V(2).Infof("RunPodSandbox called for pod %s (%s)", podName, podId)
	glog.V(3).Infof("RunPodSandbox: %s", spew.Sdump(in))
	glog.V(2).Infof("Sandbox config annotations: %v", config.GetAnnotations())

	if err := validatePodSandboxConfig(config); err != nil {
		glog.Errorf("Invalid pod config while creating pod sandbox for pod %s (%s): %v", podName, podId, err)
		return nil, err
	}

	state := kubeapi.PodSandboxState_SANDBOX_READY
	pnd := &tapmanager.PodNetworkDesc{
		PodId:   podId,
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
	netConfigBytes, err := v.fdManager.AddFDs(podId, fdPayload)
	if err != nil {
		// this will cause kubelet to delete the pod sandbox and then retry
		// its creation
		state = kubeapi.PodSandboxState_SANDBOX_NOTREADY
		glog.Errorf("Error when adding pod %s (%s) to CNI network: %v", podName, podId, err)
	}

	psi, err := metadata.NewPodSandboxInfo(config, netConfigBytes, state, clockwork.NewRealClock())
	if err != nil {
		glog.Errorf("Error serializing bod %q (%q) sandbox configuration: %v", podName, podId, err)
		return nil, err
	}

	sandbox := v.metadataStore.PodSandbox(config.Metadata.Uid)
	if storeErr := sandbox.Save(
		func(c *metadata.PodSandboxInfo) (*metadata.PodSandboxInfo, error) {
			return psi, nil
		},
	); storeErr != nil {
		glog.Errorf("Error when creating pod sandbox for pod %s (%s): %v", podName, podId, storeErr)
		return nil, storeErr
	}

	// If we don't return PodSandboxId upon RunPodSandbox, kubelet will not retry
	// RunPodSandbox for this pod after CNI failure
	return &kubeapi.RunPodSandboxResponse{
		PodSandboxId: podId,
	}, err
}

func validatePodSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	metadata := config.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("sandbox config is missing Metadata attribute: %s", spew.Sdump(config))
	}

	return nil
}

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
		return nil, err
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

func (v *VirtletManager) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podSandboxId := in.PodSandboxId
	glog.V(2).Infof("RemovePodSandbox called for pod %s", podSandboxId)
	glog.V(3).Infof("RemovePodSandbox: %s", spew.Sdump(in))

	if err := v.metadataStore.PodSandbox(podSandboxId).Save(
		func(c *metadata.PodSandboxInfo) (*metadata.PodSandboxInfo, error) {
			return nil, nil
		},
	); err != nil {
		glog.Errorf("Error when removing pod sandbox %q: %v", podSandboxId, err)
		return nil, err
	}

	response := &kubeapi.RemovePodSandboxResponse{}
	glog.V(3).Infof("RemovePodSandbox response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) PodSandboxStatus(ctx context.Context, in *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	glog.V(3).Infof("PodSandboxStatusStatus: %s", spew.Sdump(in))
	podSandboxId := in.PodSandboxId

	sandbox := v.metadataStore.PodSandbox(podSandboxId)
	sandboxInfo, err := sandbox.Retrieve()
	if err != nil {
		glog.Errorf("Error when getting pod sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}
	status := sandboxInfo.AsPodSandboxStatus()

	netResult, err := cni.BytesToResult([]byte(sandboxInfo.CNIConfig))
	if err != nil {
		glog.Errorf("Error when unmarshaling pod network configuration for sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	ip := cni.GetPodIP(netResult)
	if ip != "" {
		status.Network = &kubeapi.PodSandboxNetworkStatus{Ip: ip}
	}

	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	glog.V(3).Infof("PodSandboxStatus response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	filter := in.GetFilter()
	glog.V(3).Infof("Listing sandboxes with filter: %s", spew.Sdump(filter))
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
	glog.V(3).Infof("ListPodSandbox response: %s", spew.Sdump(response))
	return response, nil
}

//
// Containers
//

func (v *VirtletManager) CreateContainer(ctx context.Context, in *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	config := in.GetConfig()
	podSandboxId := in.PodSandboxId
	name := config.GetMetadata().Name

	glog.V(2).Infof("CreateContainer called for name: %s", name)
	glog.V(3).Infof("CreateContainer: %s", spew.Sdump(in))
	glog.V(3).Infof("CreateContainer config: %s", spew.Sdump(config))

	// Was a container already started in this sandbox?
	// NOTE: there is no distinction between lack of key and other types of
	// errors when accessing boltdb. This will be changed when we switch to
	// storing whole marshaled sandbox metadata as json.
	remainingContainers, err := v.metadataStore.ListPodContainers(podSandboxId)
	if err != nil {
		glog.V(3).Infof("Error retrieving pod %q containers", podSandboxId)
	} else {
		for _, container := range remainingContainers {
			glog.V(3).Infof("CreateContainer: there's already a container in the sandbox (id: %s), cleaning it up", container.GetID())
			if err := v.libvirtVirtualizationTool.RemoveContainer(container.GetID()); err != nil {
				glog.Errorf("Error cleaning up the old container with id %s: %v", container.GetID(), err)
				return nil, err
			}
		}
	}

	sandboxInfo, err := v.metadataStore.PodSandbox(podSandboxId).Retrieve()
	if err != nil {
		glog.Errorf("Error when retrieving pod network configuration for sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	fdKey := podSandboxId
	vmConfig, err := libvirttools.GetVMConfig(in, sandboxInfo.CNIConfig)
	if err != nil {
		glog.Errorf("Error getting vm config for container %s: %v", name, err)
		return nil, err
	}
	if len(sandboxInfo.CNIConfig) == 0 {
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

func (v *VirtletManager) StartContainer(ctx context.Context, in *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	glog.V(2).Infof("StartContainer called for containerID: %s", in.ContainerId)
	glog.V(3).Infof("StartContainer: %s", spew.Sdump(in))

	if err := v.libvirtVirtualizationTool.StartContainer(in.ContainerId); err != nil {
		glog.Errorf("Error when starting container %s: %v", in.ContainerId, err)
		return nil, err
	}
	response := &kubeapi.StartContainerResponse{}
	return response, nil
}

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

func (v *VirtletManager) RemoveContainer(ctx context.Context, in *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	glog.V(2).Infof("RemoveContainer called for containerID: %s", in.ContainerId)
	glog.V(3).Infof("RemoveContainer: %s", spew.Sdump(in))

	if err := v.libvirtVirtualizationTool.RemoveContainer(in.ContainerId); err != nil {
		glog.Errorf("Error when removing container '%s': %v", in.ContainerId, err)
		return nil, err
	}

	response := &kubeapi.RemoveContainerResponse{}
	return response, nil
}

func (v *VirtletManager) ListContainers(ctx context.Context, in *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	filter := in.GetFilter()
	glog.V(3).Infof("Listing containers with filter: %s", spew.Sdump(filter))
	glog.V(3).Infof("ListContainers: %s", spew.Sdump(in))
	containers, err := v.libvirtVirtualizationTool.ListContainers(filter)
	if err != nil {
		glog.Errorf("Error when listing containers with filter %s: %v", spew.Sdump(filter), err)
		return nil, err
	}
	response := &kubeapi.ListContainersResponse{Containers: containers}
	glog.V(3).Infof("ListContainers response:\n%s\n", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) ContainerStatus(ctx context.Context, in *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	glog.V(3).Infof("ContainerStatus: %s", spew.Sdump(in))
	status, err := v.libvirtVirtualizationTool.ContainerStatus(in.ContainerId)
	if err != nil {
		glog.Errorf("Error when getting container '%s' status: %v", in.ContainerId, err)
		return nil, err
	}

	response := &kubeapi.ContainerStatusResponse{Status: status}
	glog.V(3).Infof("ContainerStatus response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) ExecSync(context.Context, *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	glog.Errorf("ExecSync() not implemented")
	return nil, errors.New("not implemented")
}

func (v *VirtletManager) Exec(context.Context, *kubeapi.ExecRequest) (*kubeapi.ExecResponse, error) {
	glog.Errorf("Exec() not implemented")
	return nil, errors.New("not implemented")
}

func (v *VirtletManager) Attach(ctx context.Context, req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	glog.V(3).Infof("Attach called: %s", spew.Sdump(req))
	return v.StreamServer.GetAttach(req)
}

func (v *VirtletManager) PortForward(ctx context.Context, req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	glog.Errorf("PortForward() not implemented")
	return v.StreamServer.GetPortForward(req)
}

func (v *VirtletManager) UpdateRuntimeConfig(context.Context, *kubeapi.UpdateRuntimeConfigRequest) (*kubeapi.UpdateRuntimeConfigResponse, error) {
	// we don't need to do anything here for now
	return &kubeapi.UpdateRuntimeConfigResponse{}, nil
}

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

func (v *VirtletManager) ContainerStats(ctx context.Context, in *kubeapi.ContainerStatsRequest) (*kubeapi.ContainerStatsResponse, error) {
	glog.V(2).Infof("ContainerStats: %s", spew.Sdump(in))
	return nil, errors.New("ContainerStats() not implemented")
}

func (v *VirtletManager) ListContainerStats(ctx context.Context, in *kubeapi.ListContainerStatsRequest) (*kubeapi.ListContainerStatsResponse, error) {
	glog.V(2).Infof("ListContainerStats: %s", spew.Sdump(in))
	return nil, errors.New("ListContainerStats() not implemented")
}

//
// Images
//

func (v *VirtletManager) imageFromVolume(virtVolume virt.VirtStorageVolume) (*kubeapi.Image, error) {
	imageName, err := v.metadataStore.GetImageName(virtVolume.Name())
	if err != nil {
		glog.Errorf("Error when checking for existing image with volume %q: %v", virtVolume.Name(), err)
		return nil, err
	}

	if imageName == "" {
		// the image doesn't exist
		return nil, nil
	}

	size, err := virtVolume.Size()
	if err != nil {
		return nil, err
	}

	return &kubeapi.Image{
		Id:       imageName,
		RepoTags: []string{imageName},
		Size_:    size,
	}, nil
}

func (v *VirtletManager) ListImages(ctx context.Context, in *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	virtVolumes, err := v.libvirtImageTool.ListVolumes()
	if err != nil {
		glog.Errorf("Error when listing images: %v", err)
		return nil, err
	}

	images := make([]*kubeapi.Image, 0, len(virtVolumes))
	for _, virtVolume := range virtVolumes {
		image, err := v.imageFromVolume(virtVolume)
		if err != nil {
			glog.Errorf("ListImages: error when getting image info for volume %q: %v", virtVolume.Name(), err)
			return nil, err
		}
		// skip images that aren't in virtlet db
		if image == nil {
			continue
		}
		if filter := in.GetFilter(); filter != nil {
			if filter.GetImage() != nil && filter.GetImage().Image != image.RepoTags[0] {
				continue
			}
		}
		images = append(images, image)
	}

	response := &kubeapi.ListImagesResponse{Images: images}
	glog.V(3).Infof("ListImages response: %s", spew.Sdump(response))
	return response, err
}

func (v *VirtletManager) ImageStatus(ctx context.Context, in *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	imageName := in.GetImage().Image
	volumeName, err := libvirttools.ImageNameToVolumeName(imageName)
	if err != nil {
		glog.Errorf("ImageStatus: error getting volume name for image %q: %v", imageName, err)
		return nil, err
	}

	// FIXME: avoid this check by verifying ImageAsVolumeInfo() result instead
	// (need to be able to distinguish between different libvirt errors for this)
	// This query is also done in imageFromVolumeInfo() so images
	// that have volumes but aren't in virtlet db will not be retuned
	// anyway.
	existingImageName, err := v.metadataStore.GetImageName(volumeName)
	if err != nil {
		glog.Errorf("Error when checking for existing image with volume %q: %v", volumeName, err)
		return nil, err
	}

	if existingImageName == "" {
		glog.V(3).Infof("ImageStatus: image %q not found in db, returning empty response", imageName)
		return &kubeapi.ImageStatusResponse{}, nil
	}

	volume, err := v.libvirtImageTool.ImageAsVolume(volumeName)
	if err != nil {
		glog.Errorf("Error when getting info for image %q (volume %q): %v", imageName, volumeName, err)
		return nil, err
	}

	image, err := v.imageFromVolume(volume)
	if err != nil {
		glog.Errorf("ImageStatus: error getting image info for %q (volume %q): %v", imageName, volumeName, err)
		return nil, err
	}

	// Note that after the change described in FIXME comment above
	// the image can be nil here if it's not in virtlet db, but that's ok
	response := &kubeapi.ImageStatusResponse{Image: image}
	glog.V(3).Infof("ImageStatus response: %s", spew.Sdump(response))
	return response, err
}

func (v *VirtletManager) PullImage(ctx context.Context, in *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	imageName := in.GetImage().Image
	glog.V(2).Infof("PullImage called for: %s", imageName)

	volumeName, err := libvirttools.ImageNameToVolumeName(imageName)
	if err != nil {
		glog.Errorf("PullImage: error getting volume name for image %q: %v", imageName, err)
		return nil, err
	}

	imageNameTranslator := v.getImageNameTranslator(ctx)
	if _, err = v.libvirtImageTool.PullRemoteImageToVolume(imageName, volumeName, imageNameTranslator); err != nil {
		glog.Errorf("Error when pulling image %q: %v", imageName, err)
		return nil, err
	}

	err = v.metadataStore.SetImageName(volumeName, imageName)
	if err != nil {
		glog.Errorf("Error when setting image name %q for volume %q: %v", imageName, volumeName, err)
		return nil, err
	}

	response := &kubeapi.PullImageResponse{ImageRef: imageName}
	return response, nil
}

func (v *VirtletManager) getImageNameTranslator(ctx context.Context) imagetranslation.ImageNameTranslator {
	var sources []imagetranslation.ConfigSource
	sources = append(sources, imagetranslation.NewCRDSource("kube-system"))
	if v.imageTranslationConfigsDir != "" {
		sources = append(sources, imagetranslation.NewFileConfigSource(v.imageTranslationConfigsDir))
	}
	translator := imagetranslation.NewImageNameTranslator()
	translator.LoadConfigs(ctx, sources...)
	return translator
}

func (v *VirtletManager) RemoveImage(ctx context.Context, in *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	imageName := in.GetImage().Image
	glog.V(2).Infof("RemoveImage called for: %s", imageName)

	volumeName, err := libvirttools.ImageNameToVolumeName(imageName)
	if err != nil {
		glog.Errorf("RemoveImage: error getting volume name for image %q: %v", imageName, err)
		return nil, err
	}

	if err = v.libvirtImageTool.RemoveImage(volumeName); err != nil {
		glog.Errorf("Error when removing image %q with path %q: %v", imageName, volumeName, err)
		return nil, err
	}

	if err = v.metadataStore.RemoveImage(volumeName); err != nil {
		glog.Errorf("Error removing image %q from bolt: %v", imageName, volumeName, err)
		return nil, err
	}

	response := &kubeapi.RemoveImageResponse{}
	return response, nil
}

func (v *VirtletManager) ImageFsInfo(ctx context.Context, in *kubeapi.ImageFsInfoRequest) (*kubeapi.ImageFsInfoResponse, error) {
	glog.V(2).Infof("ImageFsInfo: %s", spew.Sdump(in))
	return nil, errors.New("ImageFsInfo() not implemented")
}
