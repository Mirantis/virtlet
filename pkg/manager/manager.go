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
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/bolttools"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/utils"
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
}

func NewVirtletManager(libvirtUri string, poolName string, storageBackend string, boltEndpoint string) (*VirtletManager, error) {
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
	boltClient, err := bolttools.NewBoltClient()
	if err != nil {
		return nil, err
	}

	virtletManager := &VirtletManager{
		server:                    grpc.NewServer(),
		libvirtConnTool:           libvirtConnTool,
		libvirtImageTool:          libvirtImageTool,
		libvirtVirtualizationTool: libvirtVirtualizationTool,
		boltClient:                boltClient,
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
	glog.Infof("Sandbox config labels: %#v", in.Config.Labels)
	glog.Infof("Sandbox config annotations: %#v", in.Config.Annotations)
	// podId, err := sandbox.CreatePodSandbox(v.boltSandboxTool, in.Config)
	if err := v.boltClient.SetPodSandbox(in.Config); err != nil {
		glog.Errorf("Error when creating pod sandbox: %#v", err)
		return nil, err
	}
	podId := in.Config.Metadata.GetUid()
	response := &kubeapi.RunPodSandboxResponse{
		PodSandboxId: &podId,
	}
	glog.Infof("CreatePodSandbox response: %#v", response)
	return response, nil
}

func (v *VirtletManager) StopPodSandbox(ctx context.Context, in *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	response := &kubeapi.StopPodSandboxResponse{}
	glog.Infof("StopPodSandbox response: %#v", response)
	return response, nil
}

func (v *VirtletManager) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podId := in.GetPodSandboxId()

	if err := v.boltClient.RemovePodSandbox(podId); err != nil {
		return nil, err
	}

	devName, err := v.boltClient.RetrieveTapDevFromSandbox(podId)
	if err != nil {
		glog.Errorf("Error when getting tapdev from pod sandbox: %#v", err)
		return nil, err
	}
	if err := utils.RemovePersistentIface(devName, utils.Tap); err != nil {
		glog.Errorf("Error when removing tapdev %s: %#v", devName, err)
		return nil, err
	}

	response := &kubeapi.RemovePodSandboxResponse{}
	glog.Infof("RemovePodSandbox response: %#v", response)
	return response, nil
}

func (v *VirtletManager) PodSandboxStatus(ctx context.Context, in *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	status, err := v.boltClient.GetPodSandboxStatus(in.GetPodSandboxId())
	if err != nil {
		glog.Errorf("Error when getting pod sandbox status: %#v", err)
		return nil, err
	}
	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	glog.Infof("PodSandboxStatus response: %#v", response)
	return response, nil
}

func (v *VirtletManager) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	glog.Infof("Listing sandboxes with filter: %#v", in.Filter)
	podSandboxList, err := v.boltClient.ListPodSandbox(in.Filter)
	if err != nil {
		glog.Errorf("Error when listing pod sandboxes: %#v", err)
		return nil, err
	}
	response := &kubeapi.ListPodSandboxResponse{Items: podSandboxList}
	glog.Infof("ListPodSandbox response: %#v", response)
	return response, nil
}

func (v *VirtletManager) CreateContainer(ctx context.Context, in *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	var imageName string
	if in.Config.Image.Image != nil {
		imageName = *in.Config.Image.Image
	}

	imageFilepath, err := v.boltClient.GetImageFilepath(imageName)
	if err != nil {
		return nil, err
	}

	uuid, err := v.libvirtVirtualizationTool.CreateContainer(in, imageFilepath)
	if err != nil {
		glog.Errorf("Error when creating container: %#v", err)
		return nil, err
	}

	if err := v.boltClient.SetLabels(in.Config.Metadata.GetName(), in.Config.Labels); err != nil {
		return nil, err
	}
	if err := v.boltClient.SetAnnotations(in.Config.Metadata.GetName(), in.Config.Annotations); err != nil {
		return nil, err
	}

	response := &kubeapi.CreateContainerResponse{ContainerId: &uuid}
	glog.Infof("CreateContainer response: %#v", response)
	return response, nil
}

func (v *VirtletManager) StartContainer(ctx context.Context, in *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	if err := v.libvirtVirtualizationTool.StartContainer(*in.ContainerId); err != nil {
		glog.Errorf("Error when starting container: %#v", err)
		return nil, err
	}
	response := &kubeapi.StartContainerResponse{}
	glog.Infof("StartContainer response: %#v", response)
	return response, nil
}

func (v *VirtletManager) StopContainer(ctx context.Context, in *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	if err := v.libvirtVirtualizationTool.StopContainer(*in.ContainerId); err != nil {
		glog.Errorf("Error when stopping container: %#v", err)
		return nil, err
	}
	response := &kubeapi.StopContainerResponse{}
	glog.Infof("StopContainer response: %#v", response)
	return response, nil
}

func (v *VirtletManager) RemoveContainer(ctx context.Context, in *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	if err := v.libvirtVirtualizationTool.RemoveContainer(*in.ContainerId); err != nil {
		glog.Errorf("Error when removing container: %#v", err)
		return nil, err
	}
	response := &kubeapi.RemoveContainerResponse{}
	glog.Infof("RemoveContainer response: %#v", response)
	return response, nil
}

func (v *VirtletManager) ListContainers(ctx context.Context, in *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	glog.Infof("Listing containers with filter: %#v", in.Filter)
	containers, err := v.libvirtVirtualizationTool.ListContainers(v.boltClient, in.Filter)
	if err != nil {
		glog.Errorf("Error when listing containers: %#v", err)
		return nil, err
	}
	response := &kubeapi.ListContainersResponse{Containers: containers}
	glog.Infof("ListContainers response: %#v", response)
	return response, nil
}

func (v *VirtletManager) ContainerStatus(ctx context.Context, in *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	status, err := v.libvirtVirtualizationTool.ContainerStatus(*in.ContainerId)
	if err != nil {
		glog.Errorf("Error when getting container status: %#v", err)
		return nil, err
	}

	labels, err := v.boltClient.GetLabels(*in.ContainerId)
	if err != nil {
		glog.Errorf("Error when getting container status: %#v", err)
		return nil, err
	}
	status.Labels = labels

	annotations, err := v.boltClient.GetAnnotations(*in.ContainerId)
	if err != nil {
		glog.Errorf("Error when getting container status: %#v", err)
		return nil, err
	}
	status.Annotations = annotations

	response := &kubeapi.ContainerStatusResponse{Status: status}
	glog.Infof("ContainerStatus response: %#v", response)
	return response, nil
}

func (v *VirtletManager) Exec(kubeapi.RuntimeService_ExecServer) error {
	return fmt.Errorf("Not implemented")
}

func (v *VirtletManager) ListImages(ctx context.Context, in *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	response, err := v.libvirtImageTool.ListImages()
	if err != nil {
		glog.Errorf("Error when listing images: %#v", err)
	}
	glog.Infof("ListImages response: %#v", err)
	return response, err
}

func (v *VirtletManager) ImageStatus(ctx context.Context, in *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	var name string
	if in.Image.Image != nil {
		name = *in.Image.Image
	}

	filepath, err := v.boltClient.GetImageFilepath(name)
	if err != nil {
		glog.Errorf("Error when getting image status: %#v", err)
		return nil, err
	}
	image, err := v.libvirtImageTool.ImageStatus(filepath)
	if err != nil {
		glog.Errorf("Error when getting image status: %#v", err)
		return nil, err
	}

	response := &kubeapi.ImageStatusResponse{Image: image}
	glog.Infof("ImageStatus response: %#v", response)
	return response, err
}

func (v *VirtletManager) PullImage(ctx context.Context, in *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	var name string
	if in.Image.Image != nil {
		name = *in.Image.Image
	}

	filepath, err := v.libvirtImageTool.PullImage(name)
	if err != nil {
		glog.Errorf("Error when pulling image: %#v", err)
		return nil, err
	}
	err = v.boltClient.SetImageFilepath(name, filepath)
	if err != nil {
		glog.Errorf("Error when pulling image: %#v", err)
		return nil, err
	}

	response := &kubeapi.PullImageResponse{}
	glog.Infof("PullImage response: %#v", response)
	return response, nil
}

func (v *VirtletManager) RemoveImage(ctx context.Context, in *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	var name string
	if in.Image.Image != nil {
		name = *in.Image.Image
	}

	filepath, err := v.boltClient.GetImageFilepath(name)
	if err != nil {
		glog.Errorf("Error when removing image: %#v", err)
		return nil, err
	}
	err = v.libvirtImageTool.RemoveImage(filepath)
	if err != nil {
		glog.Errorf("Error when removing image: %#v", err)
		return nil, err
	}

	response := &kubeapi.RemoveImageResponse{}
	glog.Infof("RemoveImage response: %#v", response)
	return response, nil
}
