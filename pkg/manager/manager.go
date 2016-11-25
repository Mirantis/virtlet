/*
Copyright 2016 Mirantis

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
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/bolttools"
	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
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
	// bolt
	boltClient *bolttools.BoltClient
	// cni
	cniClient *cni.Client
}

func NewVirtletManager(libvirtUri, poolName, storageBackend, boltPath, cniPluginsDir, cniConfigsDir string) (*VirtletManager, error) {
	libvirtConnTool, err := libvirttools.NewConnectionTool(libvirtUri)
	if err != nil {
		return nil, err
	}
	libvirtImageTool, err := libvirttools.NewImageTool(libvirtConnTool.Conn, poolName, storageBackend)
	if err != nil {
		return nil, err
	}
	libvirtVirtualizationTool, err := libvirttools.NewVirtualizationTool(libvirtConnTool.Conn, "volumes", "dir")
	if err != nil {
		return nil, err
	}
	boltClient, err := bolttools.NewBoltClient(boltPath)
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
		boltClient:                boltClient,
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
		Version:           &vRuntimeAPIVersion,
		RuntimeName:       &vRuntimeName,
		RuntimeVersion:    &vRuntimeVersion,
		RuntimeApiVersion: &vRuntimeVersion,
	}, nil
}

func (v *VirtletManager) RunPodSandbox(ctx context.Context, in *kubeapi.RunPodSandboxRequest) (*kubeapi.RunPodSandboxResponse, error) {
	config := in.GetConfig()
	podId := config.GetMetadata().GetUid()
	name := config.GetMetadata().GetName()
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

	bytesNetConfig, err := cni.ResultToBytes(netConfig)
	if err != nil {
		glog.Errorf("Error during network configuration result marshaling for pod %s (%s): %v", name, podId, err)
		if secondErr := v.cniClient.RemoveSandboxFromNetwork(podId); secondErr != nil {
			glog.Errorf("Error when removing pod %s (%s) from CNI network:", name, podId, err)
		}
		return nil, err
	}
	glog.V(3).Infof("CNI configuration for pod %s (%s): %s", name, podId, string(bytesNetConfig))

	if err := v.boltClient.SetPodSandbox(config, bytesNetConfig); err != nil {
		glog.Errorf("Error when creating pod sandbox for pod %s (%s): %v", name, podId, err)
		return nil, err
	}

	response := &kubeapi.RunPodSandboxResponse{
		PodSandboxId: &podId,
	}
	return response, nil
}

func (v *VirtletManager) StopPodSandbox(ctx context.Context, in *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	podSandboxId := in.GetPodSandboxId()
	glog.V(2).Infof("StopPodSandbox called for pod %s", in.GetPodSandboxId())
	glog.V(3).Infof("StopPodSandbox: %s", spew.Sdump(in))
	if err := v.boltClient.UpdatePodState(podSandboxId, kubeapi.PodSandBoxState_NOTREADY); err != nil {
		glog.Errorf("Error when stopping pod sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	response := &kubeapi.StopPodSandboxResponse{}
	return response, nil
}

func (v *VirtletManager) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podSandboxId := in.GetPodSandboxId()
	glog.V(2).Infof("RemovePodSandbox called for pod %s", podSandboxId)
	glog.V(3).Infof("RemovePodSandbox: %s", spew.Sdump(in))

	if err := v.boltClient.RemovePodSandbox(podSandboxId); err != nil {
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
	podSandboxId := in.GetPodSandboxId()

	status, err := v.boltClient.GetPodSandboxStatus(podSandboxId)
	if err != nil {
		glog.Errorf("Error when getting pod sandbox '%s': %v", podSandboxId, err)
		return nil, err
	}

	netAsBytes, err := v.boltClient.GetPodNetworkConfigurationAsBytes(podSandboxId)
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
			status.Network = &kubeapi.PodSandboxNetworkStatus{Ip: &ip}
		}
	}

	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	glog.V(3).Infof("PodSandboxStatus response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	filter := in.GetFilter()
	glog.V(3).Infof("Listing sandboxes with filter: %s", spew.Sdump(filter))
	podSandboxList, err := v.boltClient.ListPodSandbox(filter)
	if err != nil {
		glog.Errorf("Error when listing (with filter: %s) pod sandboxes: %v", spew.Sdump(filter), err)
		return nil, err
	}
	response := &kubeapi.ListPodSandboxResponse{Items: podSandboxList}
	glog.V(3).Infof("ListPodSandbox response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) CreateContainer(ctx context.Context, in *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	config := in.GetConfig()
	podSandboxId := in.GetPodSandboxId()
	imageName := config.GetImage().GetImage()
	name := config.GetMetadata().GetName()

	glog.V(2).Infof("CreateContainer called for name: %s", name)
	glog.V(3).Infof("CreateContainer: %s", spew.Sdump(in))
	glog.V(3).Infof("CreateContainer config: %s", spew.Sdump(config))

	imageFilepath, err := v.boltClient.GetImageFilepath(imageName)
	if err != nil {
		return nil, err
	}

	netAsBytes, err := v.boltClient.GetPodNetworkConfigurationAsBytes(podSandboxId)
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
	glog.V(2).Infof("CreateContainer: imageName %s, imageFilepath %s, ip %s, network namespace %s", imageName, imageFilepath, netResult.IP4.IP.IP.String(), netNSPath)

	// TODO: we should not pass whole "in" to CreateContainer - we should pass there only needed info for CreateContainer
	// without whole data container
	// TODO: use network configuration by CreateContainer
	uuid, err := v.libvirtVirtualizationTool.CreateContainer(v.boltClient, in, imageFilepath)
	if err != nil {
		glog.Errorf("Error when creating container %s: %v", name, err)
		return nil, err
	}

	response := &kubeapi.CreateContainerResponse{ContainerId: &uuid}
	glog.V(3).Infof("CreateContainer response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) StartContainer(ctx context.Context, in *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	containerId := in.GetContainerId()
	glog.V(2).Infof("StartContainer called for containerID: %s", containerId)
	glog.V(3).Infof("StartContainer: %s", spew.Sdump(in))

	if err := v.libvirtVirtualizationTool.StartContainer(containerId); err != nil {
		glog.Errorf("Error when starting container %s: %v", containerId, err)
		return nil, err
	}
	response := &kubeapi.StartContainerResponse{}
	return response, nil
}

func (v *VirtletManager) StopContainer(ctx context.Context, in *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	containerId := in.GetContainerId()
	glog.V(2).Infof("StopContainer called for containerID: %s", containerId)
	glog.V(3).Infof("StopContainer: %s", spew.Sdump(in))

	if err := v.libvirtVirtualizationTool.StopContainer(containerId); err != nil {
		glog.Errorf("Error when stopping container %s: %v", containerId, err)
		return nil, err
	}
	response := &kubeapi.StopContainerResponse{}
	return response, nil
}

func (v *VirtletManager) RemoveContainer(ctx context.Context, in *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	containerId := in.GetContainerId()
	glog.V(2).Infof("RemoveContainer called for containerID: %s", containerId)
	glog.V(3).Infof("RemoveContainer: %s", spew.Sdump(in))

	if err := v.libvirtVirtualizationTool.RemoveContainer(*in.ContainerId); err != nil {
		glog.Errorf("Error when removing container '%s': %v", containerId, err)
		return nil, err
	}

	if err := v.boltClient.RemoveContainer(containerId); err != nil {
		glog.Errorf("Error when removing container '%s' from bolt: %v", containerId, err)
		return nil, err
	}

	response := &kubeapi.RemoveContainerResponse{}
	return response, nil
}

func (v *VirtletManager) ListContainers(ctx context.Context, in *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	filter := in.GetFilter()
	glog.V(3).Infof("Listing containers with filter: %s", spew.Sdump(filter))
	glog.V(3).Infof("ListContainers: %s", spew.Sdump(in))
	containers, err := v.libvirtVirtualizationTool.ListContainers(v.boltClient, filter)
	if err != nil {
		glog.Errorf("Error when listing containers with filter %s: %v", spew.Sdump(filter), err)
		return nil, err
	}
	response := &kubeapi.ListContainersResponse{Containers: containers}
	glog.V(3).Infof("ListContainers response:\n%s\n", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) ContainerStatus(ctx context.Context, in *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	containerId := in.GetContainerId()
	glog.V(3).Infof("ContainerStatus: %s", spew.Sdump(in))
	status, err := v.libvirtVirtualizationTool.ContainerStatus(v.boltClient, containerId)
	if err != nil {
		glog.Errorf("Error when getting container '%s' status: %v", containerId, err)
		return nil, err
	}

	response := &kubeapi.ContainerStatusResponse{Status: status}
	glog.V(3).Infof("ContainerStatus response: %s", spew.Sdump(response))
	return response, nil
}

func (v *VirtletManager) Exec(kubeapi.RuntimeService_ExecServer) error {
	glog.V(3).Infof("Exec (not imageFilepath)")
	return errors.New("not implemented")
}

func (v *VirtletManager) ListImages(ctx context.Context, in *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	images, err := v.libvirtImageTool.ListImages()
	if err != nil {
		glog.Errorf("Error when listing images: %v", err)
		return nil, err
	}
	response := &kubeapi.ListImagesResponse{Images: images}
	glog.V(3).Infof("ListImages response: %s", spew.Sdump(response))
	return response, err
}

func (v *VirtletManager) ImageStatus(ctx context.Context, in *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	name := in.GetImage().GetImage()

	filepath, err := v.boltClient.GetImageFilepath(name)
	if err != nil {
		glog.Errorf("Error when getting image '%s' filepath: %v", name, err)
		return nil, err
	}
	if filepath == "" {
		return &kubeapi.ImageStatusResponse{}, nil
	}
	image, err := v.libvirtImageTool.ImageStatus(filepath)
	if err != nil {
		glog.Errorf("Error when getting image '%s' in path '%s' status: %v", name, filepath, err)
		return nil, err
	}

	response := &kubeapi.ImageStatusResponse{Image: image}
	glog.V(3).Infof("ImageStatus response: %s", spew.Sdump(response))
	return response, err
}

func stripTagFromImageName(name string) string {
	return strings.Split(name, ":")[0]
}

func (v *VirtletManager) PullImage(ctx context.Context, in *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	name := in.GetImage().GetImage()
	glog.V(2).Infof("PullImage called for: %s", name)

	name = stripTagFromImageName(name)

	filepath, err := v.libvirtImageTool.PullImage(name)
	if err != nil {
		glog.Errorf("Error when pulling image '%s': %v", name, err)
		return nil, err
	}
	err = v.boltClient.SetImageFilepath(name, filepath)
	if err != nil {
		glog.Errorf("Error when setting filepath '%s' to image '%s': %v", filepath, name, err)
		return nil, err
	}

	response := &kubeapi.PullImageResponse{}
	return response, nil
}

func (v *VirtletManager) RemoveImage(ctx context.Context, in *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	name := in.GetImage().GetImage()
	glog.V(2).Infof("RemoveImage called for: %s", name)

	filepath, err := v.boltClient.GetImageFilepath(name)
	if err != nil {
		glog.Errorf("Error when getting filepath for image '%s': %v", name, err)
		return nil, err
	}
	if filepath == "" {
		err = errors.New("image not found in database")
		glog.Errorf("Error when getting filepath for image '%s': %v", err)
		return nil, err
	}
	err = v.libvirtImageTool.RemoveImage(filepath)
	if err != nil {
		glog.Errorf("Error when removing image '%s' with path '%s': %v", name, filepath, err)
		return nil, err
	}

	response := &kubeapi.RemoveImageResponse{}
	return response, nil
}
