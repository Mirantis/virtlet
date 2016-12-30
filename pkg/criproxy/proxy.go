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
	"errors"
	"fmt"
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

const (
	targetRuntimeAnnotationKey = "kubernetes.io/target-runtime"
)

// dial creates a net.Conn by unix socket addr.
func dial(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}

// getContextWithTimeout returns a context with timeout.
func getContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

type apiClient struct {
	runtimeapi.RuntimeServiceClient
	runtimeapi.ImageServiceClient
	conn              *grpc.ClientConn
	addr              string
	id                string
	connectionTimeout time.Duration
}

func newApiClient(addr string, connectionTimeout time.Duration) *apiClient {
	id := ""
	parts := strings.SplitN(addr, ":", 2)
	if len(parts) == 2 {
		id, addr = parts[0], parts[1]
	}
	return &apiClient{addr: addr, id: id, connectionTimeout: connectionTimeout}
}

func (c *apiClient) isPrimary() bool {
	return c.id == ""
}

func (c *apiClient) connect() error {
	glog.V(1).Infof("Connecting to runtime service %s", c.addr)
	conn, err := grpc.Dial(c.addr, grpc.WithInsecure(), grpc.WithTimeout(c.connectionTimeout), grpc.WithDialer(dial))
	if err != nil {
		return err
	}
	c.conn = conn
	c.RuntimeServiceClient = runtimeapi.NewRuntimeServiceClient(conn)
	c.ImageServiceClient = runtimeapi.NewImageServiceClient(conn)

	return nil
}

func (c *apiClient) stop() {
	if c.conn == nil {
		return
	}
	if err := c.conn.Close(); err != nil {
		glog.Errorf("Failed to close gRPC connection: %v", err)
	}
	c.conn = nil
	c.RuntimeServiceClient = nil
	c.ImageServiceClient = nil
}

func (c *apiClient) wrapError(err error) error {
	return fmt.Errorf("%q: %v", c.addr, err)
}

func (c *apiClient) imageName(unprefixedName string) string {
	if c.isPrimary() {
		return unprefixedName
	}
	return c.id + "/" + unprefixedName
}

func (c *apiClient) augmentId(id string) *string {
	if !c.isPrimary() {
		id = c.id + "__" + id
	}
	return &id
}

func (c *apiClient) annotationsMatch(annotations map[string]string) bool {
	targetRuntime, found := annotations[targetRuntimeAnnotationKey]
	if c.isPrimary() {
		return !found
	}
	return found && targetRuntime == c.id
}

func (c *apiClient) idPrefixMatches(id string) (bool, string) {
	switch {
	case c.isPrimary():
		return true, id
	case strings.HasPrefix(id, c.id+"__"):
		return true, id[len(c.id)+2:]
	default:
		return false, ""
	}
}

func (c *apiClient) imageMatches(imageName string) (bool, string) {
	switch {
	case c.isPrimary():
		return true, imageName
	case strings.HasPrefix(imageName, c.id+"/"):
		return true, imageName[len(c.id)+1:]
	default:
		return false, ""
	}
}

func (c *apiClient) prefixSandbox(unprefixedSandbox *runtimeapi.PodSandbox) *runtimeapi.PodSandbox {
	if c.isPrimary() {
		return unprefixedSandbox
	}
	sandbox := *unprefixedSandbox
	sandbox.Id = c.augmentId(unprefixedSandbox.GetId())
	return &sandbox
}

func (c *apiClient) prefixSandboxes(sandboxes []*runtimeapi.PodSandbox) []*runtimeapi.PodSandbox {
	if c.isPrimary() {
		return sandboxes
	}
	var r []*runtimeapi.PodSandbox
	for _, unprefixedSandbox := range sandboxes {
		r = append(r, c.prefixSandbox(unprefixedSandbox))
	}
	return r
}

func (c *apiClient) prefixContainer(unprefixedContainer *runtimeapi.Container) *runtimeapi.Container {
	if c.isPrimary() {
		return unprefixedContainer
	}
	container := *unprefixedContainer
	container.Id = c.augmentId(unprefixedContainer.GetId())
	container.PodSandboxId = c.augmentId(unprefixedContainer.GetPodSandboxId())
	imageName := c.imageName(unprefixedContainer.GetImage().GetImage())
	container.Image.Image = &imageName
	return &container
}

func (c *apiClient) prefixContainers(unprefixedContainers []*runtimeapi.Container) []*runtimeapi.Container {
	if c.isPrimary() {
		return unprefixedContainers
	}
	var r []*runtimeapi.Container
	for _, unprefixedContainer := range unprefixedContainers {
		r = append(r, c.prefixContainer(unprefixedContainer))
	}
	return r
}

func (c *apiClient) prefixImage(unprefixedImage *runtimeapi.Image) *runtimeapi.Image {
	if c.isPrimary() {
		return unprefixedImage
	}
	image := *unprefixedImage
	id := c.imageName(image.GetId())
	image.Id = &id
	for n, tag := range image.GetRepoTags() {
		image.RepoTags[n] = c.imageName(tag)
	}
	return &image
}

func (c *apiClient) prefixImages(images []*runtimeapi.Image) []*runtimeapi.Image {
	if c.isPrimary() {
		return images
	}
	var r []*runtimeapi.Image
	for _, unprefixedImage := range images {
		r = append(r, c.prefixImage(unprefixedImage))
	}
	return r
}

// RuntimeProxy is a gRPC implementation of internalapi.RuntimeService.
type RuntimeProxy struct {
	timeout time.Duration
	server  *grpc.Server
	conn    *grpc.ClientConn
	clients []*apiClient
}

// NewRuntimeProxy creates a new internalapi.RuntimeService.
func NewRuntimeProxy(addrs []string, connectionTimout time.Duration) *RuntimeProxy {
	proxy := &RuntimeProxy{server: grpc.NewServer()}
	for _, addr := range addrs {
		proxy.clients = append(proxy.clients, newApiClient(addr, connectionTimout))
	}
	runtimeapi.RegisterRuntimeServiceServer(proxy.server, proxy)
	runtimeapi.RegisterImageServiceServer(proxy.server, proxy)

	return proxy
}

func (r *RuntimeProxy) Connect() error {
	if len(r.clients) == 0 {
		return errors.New("no sockets specified to connect to")
	}
	if !r.clients[0].isPrimary() {
		return errors.New("the first client should be primary (no id)")
	}
	for _, client := range r.clients[1:] {
		if client.isPrimary() {
			return errors.New("only the first client should be primary (no id)")
		}
	}
	for n, client := range r.clients {
		if err := client.connect(); err != nil {
			for i := 0; i < n; i++ {
				r.clients[i].stop()
			}
			return err
		}
	}
	return nil
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
	for _, client := range r.clients {
		client.stop()
	}
	// TODO: check if the server is present
	r.server.Stop()
}

// RuntimeServiceServer methods follow

// Version returns the runtime name, runtime version and runtime API version.
func (r *RuntimeProxy) Version(ctx context.Context, in *runtimeapi.VersionRequest) (*runtimeapi.VersionResponse, error) {
	glog.Infof("ENTER: Version(): %s", spew.Sdump(in))
	resp, err := r.clients[0].Version(ctx, in)
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
	if in.GetConfig() == nil {
		glog.Errorf("FAIL: RunPodSandbox(): no sandbox config")
		return nil, errors.New("criproxy: no sandbox config")
	}
	client, err := r.clientForAnnotations(in.GetConfig().GetAnnotations())
	if err != nil {
		glog.Errorf("FAIL: RunPodSandbox(): error looking for runtime: %v", err)
		return nil, err
	}

	glog.Infof("ENTER: RunPodSandbox() [%s]: %s", client.id, spew.Sdump(in))

	resp, err := client.RunPodSandbox(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RunPodSandbox(): RunPodSandbox from runtime service failed: %v", err)
		return nil, err
	}

	resp.PodSandboxId = client.augmentId(resp.GetPodSandboxId())
	glog.Infof("LEAVE: RunPodSandbox() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// StopPodSandbox stops the sandbox. If there are any running containers in the
// sandbox, they should be forced to termination.
func (r *RuntimeProxy) StopPodSandbox(ctx context.Context, in *runtimeapi.StopPodSandboxRequest) (*runtimeapi.StopPodSandboxResponse, error) {
	client, unprefixed := r.clientForId(in.GetPodSandboxId())
	glog.Infof("ENTER: StopPodSandbox() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = &unprefixed

	resp, err := client.StopPodSandbox(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: StopPodSandbox() [%s]: StopPodSandbox %q from runtime service failed: %v", client.id, in.GetPodSandboxId(), err)
		err = client.wrapError(err)
		return nil, err
	}

	glog.Infof("LEAVE: StopPodSandbox() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// RemovePodSandbox removes the sandbox. If there are any containers in the
// sandbox, they should be forcibly removed.
func (r *RuntimeProxy) RemovePodSandbox(ctx context.Context, in *runtimeapi.RemovePodSandboxRequest) (*runtimeapi.RemovePodSandboxResponse, error) {
	client, unprefixed := r.clientForId(in.GetPodSandboxId())
	glog.Infof("ENTER: RemovePodSandbox() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = &unprefixed

	resp, err := client.RemovePodSandbox(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RemovePodSandbox() [%s]: RemovePodSandbox %q from runtime service failed: %v", client.id, in.GetPodSandboxId(), err)
		err = client.wrapError(err)
		return nil, err
	}

	glog.Infof("LEAVE: RemovePodSandbox() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// PodSandboxStatus returns ruRemoteRuntimeServicentiSandbox.
func (r *RuntimeProxy) PodSandboxStatus(ctx context.Context, in *runtimeapi.PodSandboxStatusRequest) (*runtimeapi.PodSandboxStatusResponse, error) {
	client, unprefixed := r.clientForId(in.GetPodSandboxId())
	glog.Infof("ENTER: PodSandboxStatus() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = &unprefixed

	glog.Infof("ENTER: PodSandboxStatus() [%s]: %s", client.id, spew.Sdump(in))
	resp, err := client.PodSandboxStatus(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: PodSandboxStatus(): PodSandboxStatus %q from runtime service failed: %v", in.GetPodSandboxId(), err)
		err = client.wrapError(err)
		return nil, err
	}

	resp.GetStatus().Id = client.augmentId(resp.GetStatus().GetId())
	glog.Infof("LEAVE: PodSandboxStatus() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// ListPodSandbox returns a list of PodSandboxes.
func (r *RuntimeProxy) ListPodSandbox(ctx context.Context, in *runtimeapi.ListPodSandboxRequest) (*runtimeapi.ListPodSandboxResponse, error) {
	clients := r.clients
	clientStr := "all clients"
	if in.GetFilter() != nil && in.GetFilter().Id != nil {
		client, unprefixed := r.clientForId(in.GetFilter().GetId())
		clients = []*apiClient{client}
		in.GetFilter().Id = &unprefixed
		clientStr = client.id
	}

	glog.Infof("ENTER: ListPodSandbox() [%s]: %s", clientStr, spew.Sdump(in))
	var sandboxes []*runtimeapi.PodSandbox
	for _, client := range clients {
		resp, err := client.ListPodSandbox(ctx, in)
		if err != nil {
			err = client.wrapError(err)
			glog.Errorf("FAIL: ListPodSandbox() [%s]: ListPodSandbox with filter %#v from runtime service failed: %v", clientStr, in.GetFilter(), err)
			return nil, err
		}
		sandboxes = append(sandboxes, client.prefixSandboxes(resp.GetItems())...)
	}

	resp := &runtimeapi.ListPodSandboxResponse{Items: sandboxes}
	glog.Infof("LEAVE: ListPodSandbox() [%s]: %s", clientStr, spew.Sdump(resp))
	return resp, nil
}

// CreateContainer creates a new container in the specified PodSandbox.
func (r *RuntimeProxy) CreateContainer(ctx context.Context, in *runtimeapi.CreateContainerRequest) (*runtimeapi.CreateContainerResponse, error) {
	client, unprefixedSandboxId := r.clientForId(in.GetPodSandboxId())
	glog.Infof("ENTER: CreateContainer() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = &unprefixedSandboxId

	if in.GetConfig() == nil {
		glog.Errorf("FAIL: CreateContainer() [%s]: no sandbox config", client.id)
		return nil, errors.New("criproxy: no sandbox config")
	}

	image := in.GetConfig().GetImage()
	imageClient, unprefixedImage := r.clientForImage(image)
	if imageClient != client {
		glog.Errorf("FAIL: CreateContainer() [%s]: image %q is for a wrong runtime", client.id, image.GetImage())
		return nil, fmt.Errorf("criproxy: image %q is for a wrong runtime", image.GetImage())
	}
	image.Image = &unprefixedImage

	resp, err := client.CreateContainer(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: CreateContainer() [%s]: CreateContainer in sandbox %q from runtime service failed: %v", client.id, in.GetPodSandboxId(), err)
		err = client.wrapError(err)
		return nil, err
	}

	resp.ContainerId = client.augmentId(resp.GetContainerId())
	glog.Infof("LEAVE: CreateContainer() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// StartContainer starts the container.
func (r *RuntimeProxy) StartContainer(ctx context.Context, in *runtimeapi.StartContainerRequest) (*runtimeapi.StartContainerResponse, error) {
	glog.Infof("ENTER: StartContainer(): %s", spew.Sdump(in))
	resp, err := r.clients[0].StartContainer(ctx, in)
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
	resp, err := r.clients[0].StopContainer(ctx, in)
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
	resp, err := r.clients[0].RemoveContainer(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RemoveContainer(): RemoveContainer %q from runtime service failed: %v", in.GetContainerId(), err)
		return nil, err
	}

	glog.Infof("LEAVE: RemoveContainer(): %s", spew.Sdump(resp))
	return resp, nil
}

// ListContainers lists containers by filters.
func (r *RuntimeProxy) ListContainers(ctx context.Context, in *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error) {
	clients := r.clients
	clientStr := "all clients"
	if filter := in.GetFilter(); filter != nil {
		var singleClient *apiClient
		if filter.Id != nil {
			var unprefixed string
			singleClient, unprefixed = r.clientForId(filter.GetId())
			filter.Id = &unprefixed
		}
		if filter.PodSandboxId != nil {
			anotherClient, unprefixed := r.clientForId(filter.GetPodSandboxId())
			filter.PodSandboxId = &unprefixed
			if singleClient == nil {
				singleClient = anotherClient
			} else if singleClient != anotherClient {
				// different id prefixes for sandbox & container
				return &runtimeapi.ListContainersResponse{}, nil
			}
		}
		if singleClient != nil {
			clients = []*apiClient{singleClient}
			clientStr = singleClient.id
		}
	}

	glog.Infof("ENTER: ListContainers() [%s]: %s", clientStr, spew.Sdump(in))
	var containers []*runtimeapi.Container
	for _, client := range clients {
		resp, err := client.ListContainers(ctx, in)
		if err != nil {
			err = client.wrapError(err)
			glog.Errorf("FAIL: ListContainers() [%s]: ListContainers with filter %q from runtime service failed: %v", clientStr, in.GetFilter(), err)
			return nil, err
		}
		containers = append(containers, client.prefixContainers(resp.GetContainers())...)
	}

	resp := &runtimeapi.ListContainersResponse{Containers: containers}
	glog.Infof("LEAVE: ListContainers() [%s]: %s", clientStr, spew.Sdump(resp))
	return resp, nil
}

// ContainerStatus returns the container status.
func (r *RuntimeProxy) ContainerStatus(ctx context.Context, in *runtimeapi.ContainerStatusRequest) (*runtimeapi.ContainerStatusResponse, error) {
	client, unprefixed := r.clientForId(in.GetContainerId())
	glog.Infof("ENTER: ContainerStatus() [%s]: %s", client.id, spew.Sdump(in))
	in.ContainerId = &unprefixed

	resp, err := client.ContainerStatus(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ContainerStatus() [%s]: ContainerStatus %q from runtime service failed: %v", client.id, in.GetContainerId(), err)
		err = client.wrapError(err)
		return nil, err
	}

	glog.Infof("LEAVE: ContainerStatus(): %s", spew.Sdump(resp))
	resp.GetStatus().Id = client.augmentId(resp.GetStatus().GetId())
	imageName := client.imageName(resp.GetStatus().GetImage().GetImage())
	resp.GetStatus().Image.Image = &imageName
	return resp, nil
}

// ExecSync executes a command in the container, and returns the stdout output.
// If command exits with a non-zero exit code, an error is returned.
func (r *RuntimeProxy) ExecSync(ctx context.Context, in *runtimeapi.ExecSyncRequest) (*runtimeapi.ExecSyncResponse, error) {
	glog.Infof("ENTER: ExecSync(): %s", spew.Sdump(in))
	resp, err := r.clients[0].ExecSync(ctx, in)
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
	resp, err := r.clients[0].Exec(ctx, in)
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
	resp, err := r.clients[0].Attach(ctx, in)
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
	resp, err := r.clients[0].PortForward(ctx, in)
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
	resp, err := r.clients[0].UpdateRuntimeConfig(ctx, in)

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
	resp, err := r.clients[0].Status(ctx, in)
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
	clients := r.clients
	clientStr := "all clients"
	if in.GetFilter() != nil && in.GetFilter().GetImage() != nil {
		client, unprefixed := r.clientForImage(in.GetFilter().GetImage())
		clients = []*apiClient{client}
		in.GetFilter().GetImage().Image = &unprefixed
		clientStr = client.id
	}

	glog.V(3).Infof("ENTER: ListImages() [%s]: %s", clientStr, spew.Sdump(in))
	var images []*runtimeapi.Image
	for _, client := range clients {
		resp, err := client.ListImages(ctx, in)
		if err != nil {
			err = client.wrapError(err)
			glog.Errorf("FAIL: ListImages() [%s]: ListImages with filter %q from image service failed: %v", clientStr, in.GetFilter(), err)
			return nil, err
		}
		images = append(images, client.prefixImages(resp.GetImages())...)
	}

	resp := &runtimeapi.ListImagesResponse{Images: images}
	glog.V(3).Infof("LEAVE: ListImages() [%s]: %s", clientStr, spew.Sdump(resp))
	return resp, nil
}

// ImageStatus returns the status of the image.
func (r *RuntimeProxy) ImageStatus(ctx context.Context, in *runtimeapi.ImageStatusRequest) (*runtimeapi.ImageStatusResponse, error) {
	client, unprefixed := r.clientForImage(in.GetImage())
	glog.Infof("ENTER: ImageStatus() [%s]: %s", client.id, spew.Sdump(in))
	in.Image.Image = &unprefixed
	resp, err := client.ImageStatus(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: ImageStatus() [%s]: ImageStatus %q from image service failed: %v", client.id, in.GetImage().GetImage(), err)
		err = client.wrapError(err)
		return nil, err
	}

	resp.Image = client.prefixImage(resp.Image)
	glog.Infof("LEAVE: ImageStatus() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// PullImage pulls an image with authentication config.
func (r *RuntimeProxy) PullImage(ctx context.Context, in *runtimeapi.PullImageRequest) (*runtimeapi.PullImageResponse, error) {
	client, unprefixed := r.clientForImage(in.GetImage())
	glog.Infof("ENTER: PullImage() [%s]: %s", client.id, spew.Sdump(in))
	in.Image.Image = &unprefixed
	resp, err := client.PullImage(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: PullImage() [%s]: PullImage %q from image service failed: %v", client.id, in.GetImage().GetImage(), err)
		err = client.wrapError(err)
		return nil, err
	}

	glog.Infof("LEAVE: PullImage(): %s", spew.Sdump(resp))
	return resp, nil
}

// RemoveImage removes the image.
func (r *RuntimeProxy) RemoveImage(ctx context.Context, in *runtimeapi.RemoveImageRequest) (*runtimeapi.RemoveImageResponse, error) {
	client, unprefixed := r.clientForImage(in.GetImage())
	glog.Infof("ENTER: RemoveImage() [%s]: %s", client.id, spew.Sdump(in))
	in.Image.Image = &unprefixed
	resp, err := client.RemoveImage(ctx, in)
	if err != nil {
		glog.Errorf("FAIL: RemoveImage() [%s]: RemoveImage %q from image service failed: %v", client.id, in.GetImage().GetImage(), err)
		err = client.wrapError(err)
		return nil, err
	}

	glog.Infof("LEAVE: RemoveImage() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

func (r *RuntimeProxy) clientForAnnotations(annotations map[string]string) (*apiClient, error) {
	for _, client := range r.clients {
		if client.annotationsMatch(annotations) {
			return client, nil
		}
	}
	return nil, fmt.Errorf("criproxy: unknown runtime: %q", annotations[targetRuntimeAnnotationKey])
}

func (r *RuntimeProxy) clientForId(id string) (*apiClient, string) {
	for _, client := range r.clients[1:] {
		if ok, unprefixed := client.idPrefixMatches(id); ok {
			return client, unprefixed
		}
	}
	return r.clients[0], id
}

func (r *RuntimeProxy) clientForImage(image *runtimeapi.ImageSpec) (*apiClient, string) {
	for _, client := range r.clients[1:] {
		if ok, unprefixed := client.imageMatches(image.GetImage()); ok {
			return client, unprefixed
		}
	}
	return r.clients[0], image.GetImage()
}

// TODO: remove this
func init() {
	// Make spew output more readable for k8s runtime API objects
	spew.Config.DisableMethods = true
	spew.Config.DisablePointerMethods = true
}

// TODO: for primary client, show [primary] not [] in the logs
// (add apiClient.Name() or something)
