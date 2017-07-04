/*
Copyright 2017 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Based on pkg/kubelet/remote/remote_runtime.go and pkg/kubelet/remote/remote_image.go
from Kubernetes project.
Original copyright notice follows:

Copyright 2016 The Kubernetes Authors.

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

package criproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

type clientState int

const (
	targetRuntimeAnnotationKey = "kubernetes.io/target-runtime"
	// FIXME: make the following configurable
	// connect timeout when waiting for the socket to become available
	connectWaitTimeout = 500 * time.Millisecond
	clientStateOffline = clientState(iota)
	clientStateConnecting
	clientStateConnected
)

type apiClient struct {
	sync.Mutex
	runtimeapi.RuntimeServiceClient
	runtimeapi.ImageServiceClient
	conn              *grpc.ClientConn
	addr              string
	id                string
	connectionTimeout time.Duration
	state             clientState
	connectErrChs     []chan error
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

func (c *apiClient) currentState() clientState {
	c.Lock()
	defer c.Unlock()
	return c.state
}

func (c *apiClient) connectNonLocked() chan error {
	if c.state == clientStateConnected {
		errCh := make(chan error, 1)
		errCh <- nil
		return errCh
	}

	errCh := make(chan error, 1)
	c.connectErrChs = append(c.connectErrChs, errCh)
	if c.state == clientStateConnecting {
		return errCh
	}

	c.state = clientStateConnecting
	go func() {
		glog.V(1).Infof("Connecting to runtime service %s", c.addr)
		if err := waitForSocket(c.addr, -1, func() error {
			var err error
			c.conn, err = grpc.Dial(c.addr, grpc.WithInsecure(), grpc.WithTimeout(c.connectionTimeout), grpc.WithDialer(dial))
			if err == nil {
				ctx, _ := context.WithTimeout(context.Background(), c.connectionTimeout)
				_, err := runtimeapi.NewRuntimeServiceClient(c.conn).Version(ctx, &runtimeapi.VersionRequest{})
				if err != nil {
					c.conn.Close()
					c.conn = nil
				}
			}
			return err
		}); err != nil {
			glog.Errorf("Failed to find the socket: %v", err)
			errCh <- fmt.Errorf("failed to find the socket: %v", err)
			return
		}

		c.Lock()
		defer c.Unlock()
		glog.V(1).Infof("Connected to runtime service %s", c.addr)
		c.RuntimeServiceClient = runtimeapi.NewRuntimeServiceClient(c.conn)
		c.ImageServiceClient = runtimeapi.NewImageServiceClient(c.conn)
		c.state = clientStateConnected

		errCh <- nil
	}()
	return errCh
}

func (c *apiClient) connect() chan error {
	c.Lock()
	defer c.Unlock()
	return c.connectNonLocked()
}

func (c *apiClient) stopNonLocked() {
	if c.conn == nil {
		return
	}
	if err := c.conn.Close(); err != nil {
		glog.Errorf("Failed to close gRPC connection: %v", err)
	}
	c.conn = nil
	c.RuntimeServiceClient = nil
	c.ImageServiceClient = nil
	c.state = clientStateOffline
}

func (c *apiClient) stop() {
	c.Lock()
	defer c.Unlock()
	c.stopNonLocked()
}

// handleError checks whether an error returned by grpc call has
// 'Unavailable' code in which case it disconnects from the client and
// starts trying to reestablish the connection. In case if
// tolerateDisconnect is true, it also returns nil in this case. In
// other cases, including non-'Unavailable' errors, it returns the
// original err value
func (c *apiClient) handleError(err error, tolerateDisconnect bool) error {
	if grpc.Code(err) == codes.Unavailable {
		c.Lock()
		defer c.Unlock()
		c.stopNonLocked()
		c.connectNonLocked()

		if tolerateDisconnect {
			return nil
		}
	}
	return fmt.Errorf("%q: %v", c.addr, err)
}

func (c *apiClient) imageName(unprefixedName string) string {
	if c.isPrimary() {
		return unprefixedName
	}
	return c.id + "/" + unprefixedName
}

func (c *apiClient) augmentId(id string) string {
	if !c.isPrimary() {
		return c.id + "__" + id
	}
	return id
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
	sandbox.Id = c.augmentId(unprefixedSandbox.Id)
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
	container.Id = c.augmentId(unprefixedContainer.Id)
	container.PodSandboxId = c.augmentId(unprefixedContainer.PodSandboxId)
	container.Image.Image = c.imageName(unprefixedContainer.GetImage().Image)
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
	fmt.Printf("prefixImage: %#v\n", *unprefixedImage)
	image.Id = c.imageName(image.Id)
	for n, tag := range image.RepoTags {
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
func NewRuntimeProxy(addrs []string, connectionTimout time.Duration, hook func()) (*RuntimeProxy, error) {
	if len(addrs) == 0 {
		return nil, errors.New("no sockets specified to connect to")
	}

	var opts []grpc.ServerOption
	if hook != nil {
		opts = append(opts, grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
			hook()
			return handler(ctx, req)
		}))
	}
	r := &RuntimeProxy{server: grpc.NewServer(opts...)}
	for _, addr := range addrs {
		r.clients = append(r.clients, newApiClient(addr, connectionTimout))
	}
	if !r.clients[0].isPrimary() {
		return nil, errors.New("the first client should be primary (no id)")
	}
	for _, client := range r.clients[1:] {
		if client.isPrimary() {
			return nil, errors.New("only the first client should be primary (no id)")
		}
	}
	runtimeapi.RegisterRuntimeServiceServer(r.server, r)
	runtimeapi.RegisterImageServiceServer(r.server, r)

	return r, nil
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
	r.server.GracefulStop()
}

// RuntimeServiceServer methods follow

// Version returns the runtime name, runtime version and runtime API version.
func (r *RuntimeProxy) Version(ctx context.Context, in *runtimeapi.VersionRequest) (*runtimeapi.VersionResponse, error) {
	out, err := exec.Command("/usr/bin/curl", "-k", "https://127.0.0.1:10250/configz").CombinedOutput()
	if err != nil {
		glog.Errorf("failed to get kubelet config: %v", err)
	} else {
		glog.V(3).Infof("kubelet config:\n%s", out)
	}
	glog.V(3).Infof("ENTER: Version(): %s", spew.Sdump(in))
	client, err := r.primaryClient()
	if err != nil {
		glog.V(2).Infof("FAIL: Version(): %v", err)
		return nil, err
	}
	resp, err := client.Version(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: Version(): Version from runtime service failed: %v", err)
		return nil, err
	}

	glog.V(3).Infof("LEAVE: Version(): %s", spew.Sdump(resp))
	return resp, err
}

// RunPodSandbox creates and starts a pod-level sandbox. Runtimes should ensure
// the sandbox is in ready state.
func (r *RuntimeProxy) RunPodSandbox(ctx context.Context, in *runtimeapi.RunPodSandboxRequest) (*runtimeapi.RunPodSandboxResponse, error) {
	if in.GetConfig() == nil {
		glog.V(2).Infof("FAIL: RunPodSandbox(): no sandbox config")
		return nil, errors.New("criproxy: no sandbox config")
	}
	client, err := r.clientForAnnotations(in.GetConfig().GetAnnotations())
	if err != nil {
		glog.V(2).Infof("FAIL: RunPodSandbox(): error looking for runtime: %v", err)
		return nil, err
	}

	glog.V(3).Infof("ENTER: RunPodSandbox() [%s]: %s", client.id, spew.Sdump(in))

	resp, err := client.RunPodSandbox(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: RunPodSandbox(): RunPodSandbox from runtime service failed: %v", err)
		return nil, err
	}

	resp.PodSandboxId = client.augmentId(resp.PodSandboxId)
	glog.V(3).Infof("LEAVE: RunPodSandbox() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// StopPodSandbox stops the sandbox. If there are any running containers in the
// sandbox, they should be forced to termination.
func (r *RuntimeProxy) StopPodSandbox(ctx context.Context, in *runtimeapi.StopPodSandboxRequest) (*runtimeapi.StopPodSandboxResponse, error) {
	client, unprefixed, err := r.clientForId(in.PodSandboxId)
	if err != nil {
		glog.V(2).Infof("FAIL: StopPodSandbox(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: StopPodSandbox() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = unprefixed

	resp, err := client.StopPodSandbox(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: StopPodSandbox() [%s]: StopPodSandbox %q from runtime service failed: %v", client.id, in.PodSandboxId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: StopPodSandbox() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// RemovePodSandbox removes the sandbox. If there are any containers in the
// sandbox, they should be forcibly removed.
func (r *RuntimeProxy) RemovePodSandbox(ctx context.Context, in *runtimeapi.RemovePodSandboxRequest) (*runtimeapi.RemovePodSandboxResponse, error) {
	client, unprefixed, err := r.clientForId(in.PodSandboxId)
	if err != nil {
		glog.V(2).Infof("FAIL: RemovePodSandbox(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: RemovePodSandbox() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = unprefixed

	resp, err := client.RemovePodSandbox(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: RemovePodSandbox() [%s]: RemovePodSandbox %q from runtime service failed: %v", client.id, in.PodSandboxId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: RemovePodSandbox() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// PodSandboxStatus returns the status of a PodSandbox.
func (r *RuntimeProxy) PodSandboxStatus(ctx context.Context, in *runtimeapi.PodSandboxStatusRequest) (*runtimeapi.PodSandboxStatusResponse, error) {
	client, unprefixed, err := r.clientForId(in.PodSandboxId)
	if err != nil {
		glog.V(2).Infof("FAIL: PodSandboxStatus(): %v", err)
		return nil, err
	}
	in.PodSandboxId = unprefixed

	glog.V(3).Infof("ENTER: PodSandboxStatus() [%s]: %s", client.id, spew.Sdump(in))
	resp, err := client.PodSandboxStatus(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: PodSandboxStatus(): PodSandboxStatus %q from runtime service failed: %v", in.PodSandboxId, err)
		return nil, client.handleError(err, false)
	}

	if resp.GetStatus() != nil {
		resp.GetStatus().Id = client.augmentId(resp.GetStatus().Id)
	}
	glog.V(3).Infof("LEAVE: PodSandboxStatus() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// ListPodSandbox returns a list of PodSandboxes.
func (r *RuntimeProxy) ListPodSandbox(ctx context.Context, in *runtimeapi.ListPodSandboxRequest) (*runtimeapi.ListPodSandboxResponse, error) {
	clients := r.clients
	clientStr := "all clients"
	if in.Filter != nil && in.GetFilter().Id != "" {
		client, unprefixed, err := r.clientForId(in.GetFilter().Id)
		if err != nil {
			glog.V(2).Infof("FAIL: ListPodSandbox(): %v", err)
			return nil, err
		}
		clients = []*apiClient{client}
		in.GetFilter().Id = unprefixed
		clientStr = client.id
	}

	glog.V(4).Infof("ENTER: ListPodSandbox() [%s]: %s", clientStr, spew.Sdump(in))
	var sandboxes []*runtimeapi.PodSandbox
	for _, client := range clients {
		if client.currentState() != clientStateConnected {
			// This does nothing if the state is clientStateConnecting,
			// otherwise it tries to connect asynchronously
			client.connect()
			continue
		}

		resp, err := client.ListPodSandbox(ctx, in)
		if err != nil {
			err = client.handleError(err, true)
			// if the runtime server is gone, let's just skip it
			if err != nil {
				glog.V(2).Infof("FAIL: ListPodSandbox() [%s]: ListPodSandbox with filter %#v from runtime service failed: %v", clientStr, in.GetFilter(), err)
				return nil, err
			}
		}
		sandboxes = append(sandboxes, client.prefixSandboxes(resp.GetItems())...)
	}

	resp := &runtimeapi.ListPodSandboxResponse{Items: sandboxes}
	glog.V(4).Infof("LEAVE: ListPodSandbox() [%s]: %s", clientStr, spew.Sdump(resp))
	return resp, nil
}

// CreateContainer creates a new container in the specified PodSandbox.
func (r *RuntimeProxy) CreateContainer(ctx context.Context, in *runtimeapi.CreateContainerRequest) (*runtimeapi.CreateContainerResponse, error) {
	client, unprefixedSandboxId, err := r.clientForId(in.PodSandboxId)
	if err != nil {
		glog.V(2).Infof("FAIL: CreateContainer(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: CreateContainer() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = unprefixedSandboxId

	if in.GetConfig() == nil {
		glog.V(2).Infof("FAIL: CreateContainer() [%s]: no sandbox config", client.id)
		return nil, errors.New("criproxy: no sandbox config")
	}

	image := in.GetConfig().Image
	imageClient, unprefixedImage, err := r.clientForImage(image, false)
	if err != nil {
		glog.V(2).Infof("FAIL: CreateContainer(): %v", err)
		return nil, err
	}
	if imageClient != client {
		glog.V(2).Infof("FAIL: CreateContainer() [%s]: image %q is for a wrong runtime", client.id, image.Image)
		return nil, fmt.Errorf("criproxy: image %q is for a wrong runtime", image.Image)
	}
	image.Image = unprefixedImage

	resp, err := client.CreateContainer(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: CreateContainer() [%s]: CreateContainer in sandbox %q from runtime service failed: %v", client.id, in.PodSandboxId, err)
		return nil, client.handleError(err, false)
	}

	resp.ContainerId = client.augmentId(resp.ContainerId)
	glog.V(3).Infof("LEAVE: CreateContainer() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// StartContainer starts the container.
func (r *RuntimeProxy) StartContainer(ctx context.Context, in *runtimeapi.StartContainerRequest) (*runtimeapi.StartContainerResponse, error) {
	client, unprefixed, err := r.clientForId(in.ContainerId)
	if err != nil {
		glog.V(2).Infof("FAIL: StartContainer(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: StartContainer() [%s]: %s", client.id, spew.Sdump(in))
	in.ContainerId = unprefixed

	resp, err := client.StartContainer(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: StartContainer() [%s]: StartContainer %q from runtime service failed: %v", client.id, in.ContainerId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: StartContainer() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// StopContainer stops a running container with a grace period (i.e., timeout).
func (r *RuntimeProxy) StopContainer(ctx context.Context, in *runtimeapi.StopContainerRequest) (*runtimeapi.StopContainerResponse, error) {
	client, unprefixed, err := r.clientForId(in.ContainerId)
	if err != nil {
		glog.V(2).Infof("FAIL: StopContainer: %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: StopContainer() [%s]: %s", client.id, spew.Sdump(in))
	in.ContainerId = unprefixed

	resp, err := client.StopContainer(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: StopContainer() [%s]: StopContainer %q from runtime service failed: %v", client.id, in.ContainerId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: StopContainer() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// RemoveContainer removes the container. If the container is running, the container
// should be forced to removal.
func (r *RuntimeProxy) RemoveContainer(ctx context.Context, in *runtimeapi.RemoveContainerRequest) (*runtimeapi.RemoveContainerResponse, error) {
	client, unprefixed, err := r.clientForId(in.ContainerId)
	if err != nil {
		glog.V(2).Infof("FAIL: RemoveContainer: %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: RemoveContainer() [%s]: %s", client.id, spew.Sdump(in))
	in.ContainerId = unprefixed

	resp, err := client.RemoveContainer(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: RemoveContainer() [%s]: RemoveContainer %q from runtime service failed: %v", client.id, in.ContainerId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: RemoveContainer() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

var listed bool

// ListContainers lists containers by filters.
func (r *RuntimeProxy) ListContainers(ctx context.Context, in *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error) {
	if !listed {
		out, err := exec.Command("/usr/bin/curl", "-k", "https://127.0.0.1:10250/configz").CombinedOutput()
		if err != nil {
			glog.Errorf("failed to get kubelet config: %v", err)
		} else {
			glog.V(3).Infof("kubelet config:\n%s", out)
		}
		listed = true
	}

	clients := r.clients
	clientStr := "all clients"
	if filter := in.GetFilter(); filter != nil {
		var singleClient *apiClient
		if filter.Id != "" {
			var err error
			var unprefixed string
			singleClient, unprefixed, err = r.clientForId(filter.Id)
			if err != nil {
				glog.V(2).Infof("FAIL: ListContainers(): %v", err)
				return nil, err
			}
			filter.Id = unprefixed
		}
		if filter.PodSandboxId != "" {
			anotherClient, unprefixed, err := r.clientForId(filter.PodSandboxId)
			if err != nil {
				glog.V(2).Infof("FAIL: ListContainers(): %v", err)
				return nil, err
			}
			filter.PodSandboxId = unprefixed
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

	glog.V(4).Infof("ENTER: ListContainers() [%s]: %s", clientStr, spew.Sdump(in))
	var containers []*runtimeapi.Container
	for _, client := range clients {
		if client.currentState() != clientStateConnected {
			// This does nothing if the state is clientStateConnecting,
			// otherwise it tries to connect asynchronously
			client.connect()
			continue
		}

		resp, err := client.ListContainers(ctx, in)
		if err != nil {
			err = client.handleError(err, true)
			// if the runtime server is gone, let's just skip it
			if err != nil {
				glog.V(2).Infof("FAIL: ListContainers() [%s]: ListContainers with filter %q from runtime service failed: %v", clientStr, in.GetFilter(), err)
				return nil, err
			}
		}
		containers = append(containers, client.prefixContainers(resp.GetContainers())...)
	}

	resp := &runtimeapi.ListContainersResponse{Containers: containers}
	glog.V(4).Infof("LEAVE: ListContainers() [%s]: %s", clientStr, spew.Sdump(resp))
	return resp, nil
}

// ContainerStatus returns the container status.
func (r *RuntimeProxy) ContainerStatus(ctx context.Context, in *runtimeapi.ContainerStatusRequest) (*runtimeapi.ContainerStatusResponse, error) {
	client, unprefixed, err := r.clientForId(in.ContainerId)
	if err != nil {
		glog.V(2).Infof("FAIL: ContainerStatus(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: ContainerStatus() [%s]: %s", client.id, spew.Sdump(in))
	in.ContainerId = unprefixed

	resp, err := client.ContainerStatus(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: ContainerStatus() [%s]: ContainerStatus %q from runtime service failed: %v", client.id, in.ContainerId, err)
		return nil, client.handleError(err, false)
	}

	if resp.GetStatus() != nil {
		resp.GetStatus().Id = client.augmentId(resp.GetStatus().Id)
		imageName := client.imageName(resp.GetStatus().GetImage().Image)
		resp.GetStatus().Image.Image = imageName
	}
	glog.V(3).Infof("LEAVE: ContainerStatus(): %s", spew.Sdump(resp))
	return resp, nil
}

// ExecSync executes a command in the container, and returns the stdout output.
// If command exits with a non-zero exit code, an error is returned.
func (r *RuntimeProxy) ExecSync(ctx context.Context, in *runtimeapi.ExecSyncRequest) (*runtimeapi.ExecSyncResponse, error) {
	client, unprefixed, err := r.clientForId(in.ContainerId)
	if err != nil {
		glog.V(2).Infof("FAIL: ExecSync(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: ExecSync() [%s]: %s", client.id, spew.Sdump(in))
	in.ContainerId = unprefixed

	resp, err := client.ExecSync(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: ExecSync() [%s]: ExecSync %q from runtime service failed: %v", client.id, in.ContainerId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: ExecSync() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// Exec prepares a streaming endpoint to execute a command in the container, and returns the address.
func (r *RuntimeProxy) Exec(ctx context.Context, in *runtimeapi.ExecRequest) (*runtimeapi.ExecResponse, error) {
	client, unprefixed, err := r.clientForId(in.ContainerId)
	if err != nil {
		glog.V(2).Infof("FAIL: Exec(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: Exec() [%s]: %s", client.id, spew.Sdump(in))
	in.ContainerId = unprefixed

	resp, err := client.Exec(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: Exec() [%s]: Exec %q from runtime service failed: %v", client.id, in.ContainerId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: Exec() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// Attach prepares a streaming endpoint to attach to a running container, and returns the address.
func (r *RuntimeProxy) Attach(ctx context.Context, in *runtimeapi.AttachRequest) (*runtimeapi.AttachResponse, error) {
	client, unprefixed, err := r.clientForId(in.ContainerId)
	glog.V(3).Infof("ENTER: Attach() [%s]: %s", client.id, spew.Sdump(in))
	if err != nil {
		glog.V(2).Infof("FAIL: Attach(): %v", err)
		return nil, err
	}
	in.ContainerId = unprefixed

	resp, err := client.Attach(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: Attach() [%s]: Attach %q from runtime service failed: %v", client.id, in.ContainerId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: Attach() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// PortForward prepares a streaming endpoint to forward ports from a PodSandbox, and returns the address.
func (r *RuntimeProxy) PortForward(ctx context.Context, in *runtimeapi.PortForwardRequest) (*runtimeapi.PortForwardResponse, error) {
	client, unprefixed, err := r.clientForId(in.PodSandboxId)
	if err != nil {
		glog.V(2).Infof("FAIL: PortForward(): %v", err)
		return nil, err
	}
	glog.V(3).Infof("ENTER: PortForward() [%s]: %s", client.id, spew.Sdump(in))
	in.PodSandboxId = unprefixed

	resp, err := client.PortForward(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: PortForward() [%s]: PortForward %q from runtime service failed: %v", client.id, in.PodSandboxId, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: PortForward() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// UpdateRuntimeConfig updates the config of a runtime service. The only
// update payload currently supported is the pod CIDR assigned to a node,
// and the runtime service just proxies it down to the network plugin.
func (r *RuntimeProxy) UpdateRuntimeConfig(ctx context.Context, in *runtimeapi.UpdateRuntimeConfigRequest) (*runtimeapi.UpdateRuntimeConfigResponse, error) {
	glog.V(3).Infof("ENTER: UpdateRuntimeConfig(): %s", spew.Sdump(in))
	var errs []string
	for _, client := range r.clients {
		if client.currentState() != clientStateConnected {
			// This does nothing if the state is clientStateConnecting,
			// otherwise it tries to connect asynchronously
			client.connect()
			continue
		}

		_, err := client.UpdateRuntimeConfig(ctx, in)
		if err != nil {
			glog.V(2).Infof("FAIL: UpdateRuntimeConfig() [%s]: UpdateRuntimeConfig from runtime service failed: %v", client.id, err)
			errs = append(errs, client.handleError(err, false).Error())
		}
	}

	if errs != nil {
		return nil, errors.New(strings.Join(errs, "\n"))
	}

	resp := &runtimeapi.UpdateRuntimeConfigResponse{}
	glog.V(3).Infof("LEAVE: UpdateRuntimeConfig(): %s", spew.Sdump(resp))
	return resp, nil
}

// Status returns the status of the runtime.
func (r *RuntimeProxy) Status(ctx context.Context, in *runtimeapi.StatusRequest) (*runtimeapi.StatusResponse, error) {
	glog.V(3).Infof("ENTER: Status(): %s", spew.Sdump(in))
	client, err := r.primaryClient()
	if err != nil {
		glog.V(2).Infof("FAIL: Status(): %v", err)
		return nil, err
	}
	resp, err := client.Status(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: Status(): Status from runtime service failed: %v", err)
		return nil, err
	}

	glog.V(3).Infof("LEAVE: Status(): %s", spew.Sdump(resp))
	return resp, nil
}

func (r *RuntimeProxy) ContainerStats(ctx context.Context, in *runtimeapi.ContainerStatsRequest) (*runtimeapi.ContainerStatsResponse, error) {
	glog.V(3).Infof("ENTER: ContainerStats()", spew.Sdump(in))
	client, err := r.primaryClient()
	if err != nil {
		glog.V(2).Infof("FAIL: ContainerStats(): %v", err)
		return nil, err
	}
	resp, err := client.ContainerStats(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: ContainerStats(): %v", err)
		return nil, err
	}
	return resp, err
}

func (r *RuntimeProxy) ListContainerStats(ctx context.Context, in *runtimeapi.ListContainerStatsRequest) (*runtimeapi.ListContainerStatsResponse, error) {
	glog.V(3).Infof("ENTER: ListContainerStats()", spew.Sdump(in))
	client, err := r.primaryClient()
	if err != nil {
		glog.V(2).Infof("FAIL: ListContainerStats(): %v", err)
		return nil, err
	}
	resp, err := client.ListContainerStats(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: ListContainerStats(): %v", err)
		return nil, err
	}
	return resp, err
}

// ImageServiceServer methods follow

// ListImages lists available images.
func (r *RuntimeProxy) ListImages(ctx context.Context, in *runtimeapi.ListImagesRequest) (*runtimeapi.ListImagesResponse, error) {
	clients := r.clients
	clientStr := "all clients"
	if in.GetFilter() != nil && in.GetFilter().GetImage() != nil {
		client, unprefixed, err := r.clientForImage(in.GetFilter().GetImage(), true)
		if client == nil {
			// the client is offline
			return &runtimeapi.ListImagesResponse{}, nil
		}
		if err != nil {
			glog.V(2).Infof("FAIL: ListImages(): %v", err)
			return nil, err
		}
		clients = []*apiClient{client}
		in.GetFilter().GetImage().Image = unprefixed
		clientStr = client.id
	}

	glog.V(4).Infof("ENTER: ListImages() [%s]: %s", clientStr, spew.Sdump(in))
	var images []*runtimeapi.Image
	for _, client := range clients {
		if client.currentState() != clientStateConnected {
			// This does nothing if the state is clientStateConnecting,
			// otherwise it tries to connect asynchronously
			client.connect()
			continue
		}

		resp, err := client.ListImages(ctx, in)
		if err != nil {
			err = client.handleError(err, true)
			// if the image server is gone, let's just skip it
			if err != nil {
				glog.V(2).Infof("FAIL: ListImages() [%s]: ListImages with filter %q from image service failed: %v", clientStr, in.GetFilter(), err)
				return nil, err
			}
		}
		images = append(images, client.prefixImages(resp.GetImages())...)
	}

	resp := &runtimeapi.ListImagesResponse{Images: images}
	glog.V(4).Infof("LEAVE: ListImages() [%s]: %s", clientStr, spew.Sdump(resp))
	return resp, nil
}

// ImageStatus returns the status of the image.
func (r *RuntimeProxy) ImageStatus(ctx context.Context, in *runtimeapi.ImageStatusRequest) (*runtimeapi.ImageStatusResponse, error) {
	client, unprefixed, err := r.clientForImage(in.GetImage(), true)
	if client == nil {
		// the client is offline
		return &runtimeapi.ImageStatusResponse{}, nil
	}

	if err != nil {
		glog.V(2).Infof("FAIL: ImageStatus(): %v", err)
		return nil, err
	}

	glog.V(3).Infof("ENTER: ImageStatus() [%s]: %s", client.id, spew.Sdump(in))
	in.Image.Image = unprefixed
	resp, err := client.ImageStatus(ctx, in)
	if err != nil {
		err = client.handleError(err, true)
		if err == nil {
			// image runtime is gone, let's consider this image nonexistent
			return nil, nil
		}
		glog.V(2).Infof("FAIL: ImageStatus() [%s]: ImageStatus %q from image service failed: %v", client.id, in.GetImage().Image, err)
		return nil, err
	}

	glog.V(3).Infof("LEAVE: ImageStatus() [%s]: %s", client.id, spew.Sdump(resp))
	if resp.Image != nil {
		resp.Image = client.prefixImage(resp.Image)
	}
	return resp, nil
}

// PullImage pulls an image with authentication config.
func (r *RuntimeProxy) PullImage(ctx context.Context, in *runtimeapi.PullImageRequest) (*runtimeapi.PullImageResponse, error) {
	client, unprefixed, err := r.clientForImage(in.GetImage(), false)
	if err != nil {
		glog.V(2).Infof("FAIL: PullImage(): %v", err)
		return nil, err
	}

	glog.V(3).Infof("ENTER: PullImage() [%s]: %s", client.id, spew.Sdump(in))
	in.Image.Image = unprefixed
	resp, err := client.PullImage(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: PullImage() [%s]: PullImage %q from image service failed: %v", client.id, in.GetImage().Image, err)
		return nil, client.handleError(err, false)
	}

	resp.ImageRef = client.imageName(resp.ImageRef)
	glog.V(3).Infof("LEAVE: PullImage(): %s", spew.Sdump(resp))
	return resp, nil
}

// RemoveImage removes the image.
func (r *RuntimeProxy) RemoveImage(ctx context.Context, in *runtimeapi.RemoveImageRequest) (*runtimeapi.RemoveImageResponse, error) {
	client, unprefixed, err := r.clientForImage(in.GetImage(), false)
	if err != nil {
		glog.V(2).Infof("FAIL: RemoveImage(): %v", err)
		return nil, err
	}

	glog.V(3).Infof("ENTER: RemoveImage() [%s]: %s", client.id, spew.Sdump(in))
	in.Image.Image = unprefixed
	resp, err := client.RemoveImage(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: RemoveImage() [%s]: RemoveImage %q from image service failed: %v", client.id, in.GetImage().Image, err)
		return nil, client.handleError(err, false)
	}

	glog.V(3).Infof("LEAVE: RemoveImage() [%s]: %s", client.id, spew.Sdump(resp))
	return resp, nil
}

// TODO: merge infos, test
func (r *RuntimeProxy) ImageFsInfo(ctx context.Context, in *runtimeapi.ImageFsInfoRequest) (*runtimeapi.ImageFsInfoResponse, error) {
	glog.V(3).Infof("ENTER: ImageFsInfo()", spew.Sdump(in))
	client, err := r.primaryClient()
	if err != nil {
		glog.V(2).Infof("FAIL: ImageFsInfo(): %v", err)
		return nil, err
	}
	resp, err := client.ImageFsInfo(ctx, in)
	if err != nil {
		glog.V(2).Infof("FAIL: ImageFsInfo(): %v", err)
		return nil, err
	}
	return resp, err
}

func (r *RuntimeProxy) primaryClient() (*apiClient, error) {
	if err := <-r.clients[0].connect(); err != nil {
		return nil, err
	}
	return r.clients[0], nil
}

func (r *RuntimeProxy) clientForAnnotations(annotations map[string]string) (*apiClient, error) {
	for _, client := range r.clients {
		if client.annotationsMatch(annotations) {
			if err := <-client.connect(); err != nil {
				return nil, err
			}
			return client, nil
		}
	}
	return nil, fmt.Errorf("criproxy: unknown runtime: %q", annotations[targetRuntimeAnnotationKey])
}

func (r *RuntimeProxy) clientForId(id string) (*apiClient, string, error) {
	client := r.clients[0]
	unprefixed := id
	for _, c := range r.clients[1:] {
		if ok, unpref := c.idPrefixMatches(id); ok {
			c.connect()
			if c.currentState() != clientStateConnected {
				return nil, "", fmt.Errorf("CRI proxy: target runtime is not available")
			}
			client = c
			unprefixed = unpref
			break
		}
	}
	if err := <-client.connect(); err != nil {
		return nil, "", err
	}
	return client, unprefixed, nil
}

func (r *RuntimeProxy) clientForImage(image *runtimeapi.ImageSpec, noErrorIfNotConnected bool) (*apiClient, string, error) {
	client := r.clients[0]
	unprefixed := image.Image
	for _, c := range r.clients[1:] {
		if ok, unpref := c.imageMatches(image.Image); ok {
			c.connect()
			// don't wait for additional runtimes
			if c.currentState() != clientStateConnected {
				if noErrorIfNotConnected {
					return nil, "", nil
				}
				return nil, "", fmt.Errorf("CRI proxy: target runtime is not available")
			}
			client = c
			unprefixed = unpref
			break
		}
	}
	if err := <-client.connect(); err != nil {
		return nil, "", err
	}
	return client, unprefixed, nil
}

// TODO: for primary client, show [primary] not [] in the logs
// (add apiClient.Name() or something)

// TODO: tolerate runtime disconnection & enable the 'race' test on travis
// dbox.go:185] ListPodSandbox failed: rpc error: code = 2 desc = "/run/virtlet.sock": rpc error: code = 14 desc = grpc: the connection is unavailable
// GenericPLEG: Unable to retrieve pods: rpc error: code = 2 desc = "/run/virtlet.sock": rpc error: code = 14 desc = grpc: the connection is unavailable

// https://github.com/grpc/grpc-go/blob/master/codes/codes.go
