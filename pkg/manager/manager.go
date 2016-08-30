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

	"github.com/Mirantis/virtlet/pkg/etcdtools"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/sandbox"
)

const (
	runtimeAPIVersion = "0.1.0"
	runtimeName       = "virtlet"
	runtimeVersion    = "0.1.0"
)

type VirtletManager struct {
	server *grpc.Server
	// libvirt
	connTool  *libvirttools.ConnectionTool
	imageTool *libvirttools.ImageTool
	// etcd
	keysAPITool *etcdtools.KeysAPITool
	sandboxTool *etcdtools.SandboxTool
}

func NewVirtletManager(libvirtUri string, poolName string, storageBackend string, etcdEndpoint string) (*VirtletManager, error) {
	connTool, err := libvirttools.NewConnectionTool(libvirtUri)
	if err != nil {
		return nil, err
	}
	imageTool, err := libvirttools.NewImageTool(connTool.Conn, poolName, storageBackend)
	if err != nil {
		return nil, err
	}
	// TODO(nhlfr): Use many endpoints of etcd.
	keysAPITool, err := etcdtools.NewKeysAPI([]string{etcdEndpoint})
	if err != nil {
		return nil, err
	}
	sandboxTool := etcdtools.NewSandboxTool(keysAPITool.KeysAPI)

	virtletManager := &VirtletManager{
		server:      grpc.NewServer(),
		connTool:    connTool,
		imageTool:   imageTool,
		keysAPITool: keysAPITool,
		sandboxTool: sandboxTool,
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

func (v *VirtletManager) CreatePodSandbox(ctx context.Context, in *kubeapi.CreatePodSandboxRequest) (*kubeapi.CreatePodSandboxResponse, error) {
	podId, err := sandbox.CreatePodSandbox(v.sandboxTool, in.Config)
	if err != nil {
		glog.Errorf("Error when creating pod sandbox: %#v", err)
		return nil, err
	}
	response := &kubeapi.CreatePodSandboxResponse{
		PodSandboxId: &podId,
	}
	glog.Infof("CreatePodSandbox response: %#v", response)
	return response, nil
}

func (v *VirtletManager) StopPodSandbox(ctx context.Context, in *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	return &kubeapi.StopPodSandboxResponse{}, nil
}

func (v *VirtletManager) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	return &kubeapi.RemovePodSandboxResponse{}, nil
}

func (v *VirtletManager) PodSandboxStatus(ctx context.Context, in *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	status, err := v.sandboxTool.PodSandboxStatus(in.GetPodSandboxId())
	if err != nil {
		glog.Errorf("Error when getting pod sandbox status: %#v", err)
		return nil, err
	}
	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	glog.Infof("PodSandboxStatus response: %#v", response)
	return response, nil
}

func (v *VirtletManager) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	podSandboxList, err := v.sandboxTool.ListPodSandbox()
	if err != nil {
		glog.Errorf("Error when listing pod sandboxes: %#v", err)
		return nil, err
	}
	response := &kubeapi.ListPodSandboxResponse{Items: podSandboxList}
	return response, nil
}

func (v *VirtletManager) CreateContainer(ctx context.Context, in *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	return &kubeapi.CreateContainerResponse{}, nil
}

func (v *VirtletManager) StartContainer(ctx context.Context, in *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	return &kubeapi.StartContainerResponse{}, nil
}

func (v *VirtletManager) StopContainer(ctx context.Context, in *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	return &kubeapi.StopContainerResponse{}, nil
}

func (v *VirtletManager) RemoveContainer(ctx context.Context, in *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	return &kubeapi.RemoveContainerResponse{}, nil
}

func (v *VirtletManager) ListContainers(ctx context.Context, in *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	return &kubeapi.ListContainersResponse{}, nil
}

func (v *VirtletManager) ContainerStatus(ctx context.Context, in *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	return &kubeapi.ContainerStatusResponse{}, nil
}

func (v *VirtletManager) Exec(kubeapi.RuntimeService_ExecServer) error {
	return fmt.Errorf("Not implemented")
}

func (v *VirtletManager) ListImages(ctx context.Context, in *kubeapi.ListImagesRequest) (*kubeapi.ListImagesResponse, error) {
	response, err := v.imageTool.ListImages()
	if err != nil {
		glog.Errorf("Error when listing images: %#v", err)
	}
	glog.Infof("ListImages response: %#v", err)
	return response, err
}

func (v *VirtletManager) ImageStatus(ctx context.Context, in *kubeapi.ImageStatusRequest) (*kubeapi.ImageStatusResponse, error) {
	response, err := v.imageTool.ImageStatus(in)
	if err != nil {
		glog.Errorf("Error when getting image status: %#v", err)
	}
	glog.Infof("ImageStatus response: %#v", response)
	return response, err
}

func (v *VirtletManager) PullImage(ctx context.Context, in *kubeapi.PullImageRequest) (*kubeapi.PullImageResponse, error) {
	response, err := v.imageTool.PullImage(in)
	if err != nil {
		glog.Errorf("Error when pulling image: %#v", err)
	}
	glog.Infof("PullImage response: %#v", response)
	return response, err
}

func (v *VirtletManager) RemoveImage(ctx context.Context, in *kubeapi.RemoveImageRequest) (*kubeapi.RemoveImageResponse, error) {
	response, err := v.imageTool.RemoveImage(in)
	if err != nil {
		glog.Errorf("Error when removing image: %#v", err)
	}
	glog.Infof("RemoveImage response: %#v", response)
	return response, err
}
