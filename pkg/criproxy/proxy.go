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

// TODO: credits
// (based on remote_runtime.go and remote_image.go from k8s)
package criproxy

import (
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

// dial creates a net.Conn by unix socket addr.
func dial(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}

// getContextWithTimeout returns a context with timeout.
func getContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// RuntimeProxy is a gRPC implementation of internalapi.RuntimeService.
type RuntimeProxy struct {
	timeout       time.Duration
	server        *grpc.Server
	conn          *grpc.ClientConn
	runtimeClient runtimeapi.RuntimeServiceClient
	imageClient   runtimeapi.ImageServiceClient
}

// NewRuntimeProxy creates a new internalapi.RuntimeService.
func NewRuntimeProxy(addr string, connectionTimout time.Duration) (*RuntimeProxy, error) {
	// TODO: don't connect right there, use lazy connection
	glog.Infof("Connecting to runtime service %s", addr)
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(connectionTimout), grpc.WithDialer(dial))
	if err != nil {
		glog.Errorf("Connect remote runtime %s failed: %v", addr, err)
		return nil, err
	}

	proxy := &RuntimeProxy{
		server:        grpc.NewServer(),
		conn:          conn,
		runtimeClient: runtimeapi.NewRuntimeServiceClient(conn),
		imageClient:   runtimeapi.NewImageServiceClient(conn),
	}
	runtimeapi.RegisterRuntimeServiceServer(proxy.server, proxy)
	runtimeapi.RegisterImageServiceServer(proxy.server, proxy)

	return proxy, nil
}

func (r *RuntimeProxy) Serve(addr string, readyCh chan struct{}) error {
	if err := syscall.Unlink(addr); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	if readyCh != nil {
		close(readyCh)
	}
	return r.server.Serve(ln)
}

func (r *RuntimeProxy) Stop() {
	// TODO: check if conn / server is present
	if err := r.conn.Close(); err != nil {
		glog.Errorf("Failed to close gRPC connection: %v", err)
		return
	}
	r.server.Stop()
}

// RuntimeServiceServer methods follow

// Version returns the runtime name, runtime version and runtime API version.
func (r *RuntimeProxy) Version(ctx context.Context, in *runtimeapi.VersionRequest) (*runtimeapi.VersionResponse, error) {
	glog.Infof("ENTER: Version(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.Version(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: Version(): Version from runtime service failed: %v", err)
		return nil, err
	}

	glog.Infof("LEAVE: Version(): %s", spew.Sdump(resp))
	return resp, err
}

// RunPodSandbox creates and starts a pod-level sandbox. Runtimes should ensure
// the sandbox is in ready state.
func (r *RuntimeProxy) RunPodSandbox(ctx context.Context, in *runtimeapi.RunPodSandboxRequest) (*runtimeapi.RunPodSandboxResponse, error) {
	glog.Infof("ENTER: RunPodSandbox(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.RunPodSandbox(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RunPodSandbox(): RunPodSandbox from runtime service failed: %v", err)
		return nil, err
	}

	glog.Infof("LEAVE: RunPodSandbox(): %s", spew.Sdump(resp))
	return resp, nil
}

// StopPodSandbox stops the sandbox. If there are any running containers in the
// sandbox, they should be forced to termination.
func (r *RuntimeProxy) StopPodSandbox(ctx context.Context, in *runtimeapi.StopPodSandboxRequest) (*runtimeapi.StopPodSandboxResponse, error) {
	glog.Infof("ENTER: StopPodSandbox(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.StopPodSandbox(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: StopPodSandbox(): StopPodSandbox %q from runtime service failed: %v", in.GetPodSandboxId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: StopPodSandbox(): %s", spew.Sdump(resp))
	return resp, nil
}

// RemovePodSandbox removes the sandbox. If there are any containers in the
// sandbox, they should be forcibly removed.
func (r *RuntimeProxy) RemovePodSandbox(ctx context.Context, in *runtimeapi.RemovePodSandboxRequest) (*runtimeapi.RemovePodSandboxResponse, error) {
	glog.Infof("ENTER: RemovePodSandbox(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.RemovePodSandbox(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RemovePodSandbox(): RemovePodSandbox %q from runtime service failed: %v", in.GetPodSandboxId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: RemovePodSandbox(): %s", spew.Sdump(resp))
	return resp, nil
}

// PodSandboxStatus returns ruRemoteRuntimeServicentiSandbox.
func (r *RuntimeProxy) PodSandboxStatus(ctx context.Context, in *runtimeapi.PodSandboxStatusRequest) (*runtimeapi.PodSandboxStatusResponse, error) {
	glog.Infof("ENTER: PodSandboxStatus(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.PodSandboxStatus(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: PodSandboxStatus(): PodSandboxStatus %q from runtime service failed: %v", in.GetPodSandboxId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: PodSandboxStatus(): %s", spew.Sdump(resp))
	return resp, nil
}

// ListPodSandbox returns a list of PodSandboxes.
func (r *RuntimeProxy) ListPodSandbox(ctx context.Context, in *runtimeapi.ListPodSandboxRequest) (*runtimeapi.ListPodSandboxResponse, error) {
	glog.Infof("ENTER: ListPodSandbox(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.ListPodSandbox(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ListPodSandbox(): ListPodSandbox with filter %q from runtime service failed: %v", in.GetFilter(), err)
		return nil, err
	}

	glog.Infof("LEAVE: ListPodSandbox(): %s", spew.Sdump(resp))
	return resp, nil
}

// CreateContainer creates a new container in the specified PodSandbox.
func (r *RuntimeProxy) CreateContainer(ctx context.Context, in *runtimeapi.CreateContainerRequest) (*runtimeapi.CreateContainerResponse, error) {
	glog.Infof("ENTER: CreateContainer(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.CreateContainer(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: CreateContainer(): CreateContainer in sandbox %q from runtime service failed: %v", in.GetPodSandboxId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: CreateContainer(): %s", spew.Sdump(resp))
	return resp, nil
}

// StartContainer starts the container.
func (r *RuntimeProxy) StartContainer(ctx context.Context, in *runtimeapi.StartContainerRequest) (*runtimeapi.StartContainerResponse, error) {
	glog.Infof("ENTER: StartContainer(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.StartContainer(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: StartContainer(): StartContainer %q from runtime service failed: %v", in.GetContainerId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: StartContainer(): %s", spew.Sdump(resp))
	return resp, nil
}

// StopContainer stops a running container with a grace period (i.e., timeout).
func (r *RuntimeProxy) StopContainer(ctx context.Context, in *runtimeapi.StopContainerRequest) (*runtimeapi.StopContainerResponse, error) {
	glog.Infof("ENTER: StopContainer(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.StopContainer(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: StopContainer(): StopContainer %q from runtime service failed: %v", in.GetContainerId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: StopContainer(): %s", spew.Sdump(resp))
	return resp, nil
}

// RemoveContainer removes the container. If the container is running, the container
// should be forced to removal.
func (r *RuntimeProxy) RemoveContainer(ctx context.Context, in *runtimeapi.RemoveContainerRequest) (*runtimeapi.RemoveContainerResponse, error) {
	glog.Infof("ENTER: RemoveContainer(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.RemoveContainer(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RemoveContainer(): RemoveContainer %q from runtime service failed: %v", in.GetContainerId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: RemoveContainer(): %s", spew.Sdump(resp))
	return resp, nil
}

// ListContainers lists containers by filters.
func (r *RuntimeProxy) ListContainers(ctx context.Context, in *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error) {
	glog.Infof("ENTER: ListContainers(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.ListContainers(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ListContainers(): ListContainers with filter %q from runtime service failed: %v", in.GetFilter(), err)
		return nil, err
	}

	glog.Infof("LEAVE: ListContainers(): %s", spew.Sdump(resp))
	return resp, nil
}

// ContainerStatus returns the container status.
func (r *RuntimeProxy) ContainerStatus(ctx context.Context, in *runtimeapi.ContainerStatusRequest) (*runtimeapi.ContainerStatusResponse, error) {
	glog.Infof("ENTER: ContainerStatus(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.ContainerStatus(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ContainerStatus(): ContainerStatus %q from runtime service failed: %v", in.GetContainerId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: ContainerStatus(): %s", spew.Sdump(resp))
	return resp, nil
}

// ExecSync executes a command in the container, and returns the stdout output.
// If command exits with a non-zero exit code, an error is returned.
func (r *RuntimeProxy) ExecSync(ctx context.Context, in *runtimeapi.ExecSyncRequest) (*runtimeapi.ExecSyncResponse, error) {
	glog.Infof("ENTER: ExecSync(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.ExecSync(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ExecSync(): ExecSync %s '%s' from runtime service failed: %v", in.GetContainerId(), strings.Join(in.GetCmd(), " "), err)
		return nil, err
	}

	glog.Infof("LEAVE: ExecSync(): %s", spew.Sdump(resp))
	return resp, nil
}

// Exec prepares a streaming endpoint to execute a command in the container, and returns the address.
func (r *RuntimeProxy) Exec(ctx context.Context, in *runtimeapi.ExecRequest) (*runtimeapi.ExecResponse, error) {
	glog.Infof("ENTER: Exec(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.Exec(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: Exec(): Exec %s '%s' from runtime service failed: %v", in.GetContainerId(), strings.Join(in.GetCmd(), " "), err)
		return nil, err
	}

	glog.Infof("LEAVE: Exec(): %s", spew.Sdump(resp))
	return resp, nil
}

// Attach prepares a streaming endpoint to attach to a running container, and returns the address.
func (r *RuntimeProxy) Attach(ctx context.Context, in *runtimeapi.AttachRequest) (*runtimeapi.AttachResponse, error) {
	glog.Infof("ENTER: Attach(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.Attach(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: Attach(): Attach %s from runtime service failed: %v", in.GetContainerId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: Attach(): %s", spew.Sdump(resp))
	return resp, nil
}

// PortForward prepares a streaming endpoint to forward ports from a PodSandbox, and returns the address.
func (r *RuntimeProxy) PortForward(ctx context.Context, in *runtimeapi.PortForwardRequest) (*runtimeapi.PortForwardResponse, error) {
	glog.Infof("ENTER: PortForward(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.PortForward(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: PortForward(): PortForward %s from runtime service failed: %v", in.GetPodSandboxId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: PortForward(): %s", spew.Sdump(resp))
	return resp, nil
}

// UpdateRuntimeConfig updates the config of a runtime service. The only
// update payload currently supported is the pod CIDR assigned to a node,
// and the runtime service just proxies it down to the network plugin.
func (r *RuntimeProxy) UpdateRuntimeConfig(ctx context.Context, in *runtimeapi.UpdateRuntimeConfigRequest) (*runtimeapi.UpdateRuntimeConfigResponse, error) {
	glog.Infof("ENTER: UpdateRuntimeConfig(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.UpdateRuntimeConfig(ctx, in)

	if err != nil {
		glog.Errorf("FAIL: UpdateRuntimeConfig(): UpdateRuntimeConfig from runtime service failed: %v", err)
		return nil, err
	}

	glog.Infof("LEAVE: UpdateRuntimeConfig(): %s", spew.Sdump(resp))
	return resp, nil
}

// Status returns the status of the runtime.
func (r *RuntimeProxy) Status(ctx context.Context, in *runtimeapi.StatusRequest) (*runtimeapi.StatusResponse, error) {
	glog.Infof("ENTER: Status(): %s", spew.Sdump(in))
	resp, err := r.runtimeClient.Status(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: Status(): Status from runtime service failed: %v", err)
		return nil, err
	}

	glog.Infof("LEAVE: Status(): %s", spew.Sdump(resp))
	return resp, nil
}

// ImageServiceServer methods follow

// ListImages lists available images.
func (r *RuntimeProxy) ListImages(ctx context.Context, in *runtimeapi.ListImagesRequest) (*runtimeapi.ListImagesResponse, error) {
	glog.Infof("ENTER: ListImages(): %s", spew.Sdump(in))
	resp, err := r.imageClient.ListImages(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ListImages(): ListImages with filter %q from image service failed: %v", in.GetFilter(), err)
		return nil, err
	}

	glog.Infof("LEAVE: ListImages(): %s", spew.Sdump(resp))
	return resp, nil
}

// ImageStatus returns the status of the image.
func (r *RuntimeProxy) ImageStatus(ctx context.Context, in *runtimeapi.ImageStatusRequest) (*runtimeapi.ImageStatusResponse, error) {
	glog.Infof("ENTER: ImageStatus(): %s", spew.Sdump(in))
	resp, err := r.imageClient.ImageStatus(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ImageStatus(): ImageStatus %q from image service failed: %v", in.GetImage().GetImage(), err)
		return nil, err
	}

	glog.Infof("LEAVE: ImageStatus(): %s", spew.Sdump(resp))
	return resp, nil
}

// PullImage pulls an image with authentication config.
func (r *RuntimeProxy) PullImage(ctx context.Context, in *runtimeapi.PullImageRequest) (*runtimeapi.PullImageResponse, error) {
	glog.Infof("ENTER: PullImage(): %s", spew.Sdump(in))
	resp, err := r.imageClient.PullImage(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: PullImage(): PullImage %q from image service failed: %v", in.GetImage().GetImage(), err)
		return nil, err
	}

	glog.Infof("LEAVE: PullImage(): %s", spew.Sdump(resp))
	return resp, nil
}

// RemoveImage removes the image.
func (r *RuntimeProxy) RemoveImage(ctx context.Context, in *runtimeapi.RemoveImageRequest) (*runtimeapi.RemoveImageResponse, error) {
	glog.Infof("ENTER: RemoveImage(): %s", spew.Sdump(in))
	resp, err := r.imageClient.RemoveImage(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RemoveImage(): RemoveImage %q from image service failed: %v", in.GetImage().GetImage(), err)
		return nil, err
	}

	glog.Infof("LEAVE: RemoveImage(): %s", spew.Sdump(resp))
	return resp, nil
}

// TODO: remove all of the following

// var removeThis1 runtimeapi.RuntimeServiceServer = &RuntimeProxy{}
// var removeThis2 runtimeapi.ImageServiceServer = &RuntimeProxy{}

func init() {
	// Make spew output more readable for k8s runtime API objects
	spew.Config.DisableMethods = true
	spew.Config.DisablePointerMethods = true
}
