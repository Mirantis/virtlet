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

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/bolttools"
	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
)

const (
	runtimeAPIVersion = "0.1.0"
	runtimeName       = "virtlet"
	runtimeVersion    = "0.1.0"
)

type VirtletManager struct {
	server *grpc.Server
	// libvirt
	libvirtConnTool           *libvirttools.ConnectionTool
	libvirtImageTool          *libvirttools.ImageTool
	libvirtVirtualizationTool *libvirttools.VirtualizationTool
	// metadata
	metadataStore metadata.MetadataStore
	// cni
	cniClient *cni.Client
}

func NewVirtletManager(libvirtUri, poolName, downloadProtocol, storageBackend, metadataPath, cniPluginsDir, cniConfigsDir, rawDevices string) (*VirtletManager, error) {
	libvirtConnTool, err := libvirttools.NewConnectionTool(libvirtUri)
	if err != nil {
		return nil, err
	}

	libvirtImageTool, err := libvirttools.NewImageTool(libvirtConnTool.Connection(), poolName, downloadProtocol)
	if err != nil {
		return nil, err
	}

	boltClient, err := bolttools.NewBoltClient(metadataPath)
	if err != nil {
		return nil, err
	}

	// TODO: pool name should be passed like for imageTool
	libvirtVirtualizationTool, err := libvirttools.NewVirtualizationTool(libvirtConnTool.Connection(), "volumes", rawDevices, boltClient)
	if err != nil {
		return nil, err
	}

	cniClient, err := cni.NewClient(cniPluginsDir, cniConfigsDir)
	if err != nil {
		return nil, err
	}

	virtletManager := &VirtletManager{
		server:                    grpc.NewServer(),
		libvirtConnTool:           libvirtConnTool,
		libvirtImageTool:          libvirtImageTool,
		libvirtVirtualizationTool: libvirtVirtualizationTool,
		metadataStore:             boltClient,
		cniClient:                 cniClient,
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
	podId := config.GetMetadata().Uid
	name := config.GetMetadata().Name
	glog.V(2).Infof("RunPodSandbox called for pod %s (%s)", name, podId)
	glog.V(3).Infof("RunPodSandbox: %s", spew.Sdump(in))
	glog.V(2).Infof("Sandbox config annotations: %v", config.GetAnnotations())

	if err := cni.CreateNetNS(podId); err != nil {
		glog.Errorf("Error when creating new netns for pod %s (%s): %v", name, podId, err)
		return nil, err
	}

	netConfig, err := v.cniClient.AddSandboxToNetwork(podId)
	if err != nil {
		glog.Errorf("Error when adding pod %s (%s) to CNI network: %v", name, podId, err)
		return nil, err
	}

	// Mimic kubelet's method of handling nameservers.
	// As of k8s 1.5.2, kubelet doesn't use any nameserver information from CNI.
	// (TODO: recheck this for 1.6)
	// CNI is used just to configure the network namespace and CNI DNS
	// info is ignored. Instead of this, DnsConfig from PodSandboxConfig
	// is used to configure container's resolv.conf.
	if config.DnsConfig != nil {
		netConfig.DNS.Nameservers = config.DnsConfig.Servers
		netConfig.DNS.Search = config.DnsConfig.Searches
		netConfig.DNS.Options = config.DnsConfig.Options
	}

	bytesNetConfig, err := cni.ResultToBytes(netConfig)
	if err != nil {
		glog.Errorf("Error during network configuration result marshaling for pod %s (%s): %v", name, podId, err)
		if secondErr := v.cniClient.RemoveSandboxFromNetwork(podId); secondErr != nil {
			glog.Errorf("Error when removing pod %s (%s) from CNI network:", name, podId, err)
		}
		return nil, err
	}
	glog.V(3).Infof("CNI configuration for pod %s (%s): %s", name, podId, string(bytesNetConfig))

	if err := validatePodSandboxConfig(config); err != nil {
		glog.Errorf("Invalid pod config while creating pod sandbox for pod %s (%s): %v", name, podId, err)
		return nil, err
	}
	if err := v.metadataStore.SetPodSandbox(config, bytesNetConfig); err != nil {
		glog.Errorf("Error when creating pod sandbox for pod %s (%s): %v", name, podId, err)
		return nil, err
	}

	response := &kubeapi.RunPodSandboxResponse{
		PodSandboxId: podId,
	}
	return response, nil
}

func validatePodSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	metadata := config.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("sandbox config is missing Metadata attribute: %s", spew.Sdump(config))
	}

	linuxSandbox := config.GetLinux()
	if linuxSandbox == nil {
		return fmt.Errorf("sandbox config is missing Linux attribute: %s", spew.Sdump(config))
	}

	if linuxSandbox.GetSecurityContext() == nil {
		return fmt.Errorf("Linux sandbox config is missing SecurityContext attribute: %s", spew.Sdump(config))
	}

	namespaceOptions := linuxSandbox.GetSecurityContext().GetNamespaceOptions()
	if namespaceOptions == nil {
		return fmt.Errorf("SecurityContext is missing Namespaces attribute: %s", spew.Sdump(config))
	}

	return nil
}

func (v *VirtletManager) StopPodSandbox(ctx context.Context, in *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	podSandboxId := in.PodSandboxId
	glog.V(2).Infof("StopPodSandbox called for pod %s", in.PodSandboxId)
	glog.V(3).Infof("StopPodSandbox: %s", spew.Sdump(in))
	if err := v.metadataStore.UpdatePodState(podSandboxId, byte(kubeapi.PodSandboxState_SANDBOX_NOTREADY)); err != nil {
		glog.Errorf("Error when stopping pod sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	response := &kubeapi.StopPodSandboxResponse{}
	return response, nil
}

func (v *VirtletManager) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podSandboxId := in.PodSandboxId
	glog.V(2).Infof("RemovePodSandbox called for pod %s", podSandboxId)
	glog.V(3).Infof("RemovePodSandbox: %s", spew.Sdump(in))

	if err := v.metadataStore.RemovePodSandbox(podSandboxId); err != nil {
		glog.Errorf("Error when removing pod sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	if err := v.cniClient.RemoveSandboxFromNetwork(podSandboxId); err != nil {
		glog.Errorf("Error when removing pod sandbox '%s' from CNI network: %v", podSandboxId, err)
		return nil, err
	}

	if err := cni.DestroyNetNS(podSandboxId); err != nil {
		glog.Errorf("Error when removing network namespace for pod sandbox %s: %v", podSandboxId, err)
		return nil, err
	}

	response := &kubeapi.RemovePodSandboxResponse{}
	glog.V(3).Infof("RemovePodSandbox response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) PodSandboxStatus(ctx context.Context, in *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	glog.V(3).Infof("PodSandboxStatusStatus: %s", spew.Sdump(in))
	podSandboxId := in.PodSandboxId

	status, err := v.metadataStore.GetPodSandboxStatus(podSandboxId)
	if err != nil {
		glog.Errorf("Error when getting pod sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	netAsBytes, err := v.metadataStore.GetPodNetworkConfigurationAsBytes(podSandboxId)
	if err != nil {
		glog.Errorf("Error when retrieving pod network configuration for sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	if len(netAsBytes) != 0 {
		netResult, err := cni.BytesToResult(netAsBytes)
		if err != nil {
			glog.Errorf("Error when unmarshaling pod network configuration for sandbox '%s': %v", podSandboxId, err)
			return nil, err
		}

		if netResult.IP4 != nil {
			ip := netResult.IP4.IP.IP.String()
			status.Network = &kubeapi.PodSandboxNetworkStatus{Ip: ip}
		}
	}

	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	glog.V(3).Infof("PodSandboxStatus response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	filter := in.GetFilter()
	glog.V(3).Infof("Listing sandboxes with filter: %s", spew.Sdump(filter))
	podSandboxList, err := v.metadataStore.ListPodSandbox(filter)
	if err != nil {
		glog.Errorf("Error when listing (with filter: %s) pod sandboxes: %v", spew.Sdump(filter), err)
		return nil, err
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
	imageName := config.GetImage().Image
	name := config.GetMetadata().Name

	glog.V(2).Infof("CreateContainer called for name: %s", name)
	glog.V(3).Infof("CreateContainer: %s", spew.Sdump(in))
	glog.V(3).Infof("CreateContainer config: %s", spew.Sdump(config))

	volumeName, err := libvirttools.ImageNameToVolumeName(imageName)
	if err != nil {
		glog.Errorf("CreateContainer: error getting volume name for image %q: %v", imageName, err)
		return nil, err
	}

	imageFilePath, err := v.libvirtImageTool.ImageFilePath(volumeName)
	if err != nil {
		glog.Errorf("Error when getting file path for image %q (volume %q): %v", imageName, volumeName, err)
		return nil, err
	}

	volumeInfo, err := v.libvirtImageTool.ImageAsVolumeInfo(volumeName)
	if err != nil {
		glog.Errorf("Error when getting volume info for image %q (volume %q): %v", imageName, volumeName, err)
		return nil, err
	}

	// TODO: get it as string
	netAsBytes, err := v.metadataStore.GetPodNetworkConfigurationAsBytes(podSandboxId)
	if err != nil {
		glog.Errorf("Error when retrieving pod network configuration for sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	netResult, err := cni.BytesToResult(netAsBytes)
	if err != nil {
		glog.Errorf("Error when unmarshaling pod network configuration for sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	netNSPath := cni.PodNetNSPath(podSandboxId)
	glog.V(2).Infof("CreateContainer: imageName %s, imageFilepath %s, ip %s, network namespace %s", imageName, imageFilePath, netResult.IP4.IP.IP.String(), netNSPath)

	// TODO: we should not pass whole "in" to CreateContainer - we should pass there only needed info for CreateContainer
	// without whole data container
	// TODO: use network configuration by CreateContainer
	uuid, err := v.libvirtVirtualizationTool.CreateContainer(in, imageFilePath, volumeInfo.Size, netNSPath, string(netAsBytes))
	if err != nil {
		glog.Errorf("Error when creating container %s: %v", name, err)
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

	if err := v.libvirtVirtualizationTool.StopContainer(in.ContainerId); err != nil {
		glog.Errorf("Error when stopping container %s: %v", in.ContainerId, err)
		return nil, err
	}
	response := &kubeapi.StopContainerResponse{}
	glog.V(2).Info("Sending stop response for containerID: %s", in.ContainerId)
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

func (v *VirtletManager) Attach(context.Context, *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	glog.Errorf("Attach() not implemented")
	return nil, errors.New("not implemented")
}

func (v *VirtletManager) PortForward(context.Context, *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	glog.Errorf("PortForward() not implemented")
	return nil, errors.New("not implemented")
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

//
// Images
//

func (v *VirtletManager) imageFromVolumeInfo(volumeInfo *libvirttools.VolumeInfo) (*kubeapi.Image, error) {
	imageName, err := v.metadataStore.GetImageName(volumeInfo.Name)
	if err != nil {
		glog.Errorf("Error when checking for existing image with volume %q: %v", volumeInfo.Name, err)
		return nil, err
	}

	if imageName == "" {
		// the image doesn't exist
		return nil, nil
	}

	return &kubeapi.Image{
		Id:       volumeInfo.Name,
		RepoTags: []string{imageName},
		Size_:    volumeInfo.Size,
	}, nil
}

func (v *VirtletManager) ListImages(ctx context.Context, in *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	volumeInfos, err := v.libvirtImageTool.ListImagesAsVolumeInfos()
	if err != nil {
		glog.Errorf("Error when listing images: %v", err)
		return nil, err
	}

	images := make([]*kubeapi.Image, 0, len(volumeInfos))
	for _, volumeInfo := range volumeInfos {
		image, err := v.imageFromVolumeInfo(volumeInfo)
		if err != nil {
			glog.Errorf("ListImages: error when getting image info for volume %q: %v", volumeInfo.Name, err)
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

	volumeInfo, err := v.libvirtImageTool.ImageAsVolumeInfo(volumeName)
	if err != nil {
		glog.Errorf("Error when getting info for image %q (volume %q): %v", imageName, volumeName, err)
		return nil, err
	}

	image, err := v.imageFromVolumeInfo(volumeInfo)
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

	// PullPolicy is not accessible directly within remote runtime
	// But PullImage request can be called in 2 cases:
	// 1. PullAlways
	// 2. PullIfNotPresent
	// So need to check whether the image with such URL was already downloaded

	existingImageName, err := v.metadataStore.GetImageName(volumeName)
	if err != nil {
		glog.Errorf("PullImage: error when checking for existing image %q: %v", imageName, err)
		return nil, err
	}

	if existingImageName != "" {
		// Image has been downloaded already
		return &kubeapi.PullImageResponse{ImageRef: imageName}, nil
	}

	if err = v.libvirtImageTool.PullImageToVolume(imageName, volumeName); err != nil {
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
