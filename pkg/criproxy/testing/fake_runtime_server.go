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
// (based on fake_runtime_service.go and utils.go [from the same dir] from k8s)
package testing

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"golang.org/x/net/context"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

var (
	version = "0.1.0"

	FakeRuntimeName  = "fakeRuntime"
	FakePodSandboxIP = "192.168.192.168"
)

func BuildContainerName(metadata *runtimeapi.ContainerMetadata, sandboxID string) string {
	// include the sandbox ID to make the container ID unique.
	return fmt.Sprintf("%s_%s_%d", sandboxID, metadata.GetName(), metadata.GetAttempt())
}

func BuildSandboxName(metadata *runtimeapi.PodSandboxMetadata) string {
	return fmt.Sprintf("%s_%s_%s_%d", metadata.GetName(), metadata.GetNamespace(), metadata.GetUid(), metadata.GetAttempt())
}

func filterInLabels(filter, labels map[string]string) bool {
	for k, v := range filter {
		if value, ok := labels[k]; ok {
			if value != v {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

type FakePodSandbox struct {
	// PodSandboxStatus contains the runtime information for a sandbox.
	runtimeapi.PodSandboxStatus
}

type FakeContainer struct {
	// ContainerStatus contains the runtime information for a container.
	runtimeapi.ContainerStatus

	// the sandbox id of this container
	SandboxID string
}

type FakeRuntimeServer struct {
	sync.Mutex

	Called []string

	FakeStatus *runtimeapi.RuntimeStatus
	Containers map[string]*FakeContainer
	Sandboxes  map[string]*FakePodSandbox
}

func (r *FakeRuntimeServer) SetFakeSandboxes(sandboxes []*FakePodSandbox) {
	r.Lock()
	defer r.Unlock()

	r.Sandboxes = make(map[string]*FakePodSandbox)
	for _, sandbox := range sandboxes {
		sandboxID := sandbox.GetId()
		r.Sandboxes[sandboxID] = sandbox
	}
}

func (r *FakeRuntimeServer) SetFakeContainers(containers []*FakeContainer) {
	r.Lock()
	defer r.Unlock()

	r.Containers = make(map[string]*FakeContainer)
	for _, c := range containers {
		containerID := c.GetId()
		r.Containers[containerID] = c
	}

}

func (r *FakeRuntimeServer) AssertCalls(calls []string) error {
	r.Lock()
	defer r.Unlock()

	if !reflect.DeepEqual(calls, r.Called) {
		return fmt.Errorf("expected %#v, got %#v", calls, r.Called)
	}
	return nil
}

func NewFakeRuntimeServer() *FakeRuntimeServer {
	return &FakeRuntimeServer{
		Called:     make([]string, 0),
		Containers: make(map[string]*FakeContainer),
		Sandboxes:  make(map[string]*FakePodSandbox),
	}
}

func (r *FakeRuntimeServer) Version(ctx context.Context, in *runtimeapi.VersionRequest) (*runtimeapi.VersionResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "Version")

	return &runtimeapi.VersionResponse{
		Version:           &version,
		RuntimeName:       &FakeRuntimeName,
		RuntimeVersion:    &version,
		RuntimeApiVersion: &version,
	}, nil
}

func (r *FakeRuntimeServer) Status(ctx context.Context, in *runtimeapi.StatusRequest) (*runtimeapi.StatusResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "Status")

	return &runtimeapi.StatusResponse{Status: r.FakeStatus}, nil
}

func (r *FakeRuntimeServer) RunPodSandbox(ctx context.Context, in *runtimeapi.RunPodSandboxRequest) (*runtimeapi.RunPodSandboxResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "RunPodSandbox")

	// PodSandboxID should be randomized for real container runtime, but here just use
	// fixed name from BuildSandboxName() for easily making fake sandboxes.
	config := in.GetConfig()
	podSandboxID := BuildSandboxName(config.Metadata)
	createdAt := time.Now().Unix()
	readyState := runtimeapi.PodSandboxState_SANDBOX_READY
	r.Sandboxes[podSandboxID] = &FakePodSandbox{
		PodSandboxStatus: runtimeapi.PodSandboxStatus{
			Id:        &podSandboxID,
			Metadata:  config.Metadata,
			State:     &readyState,
			CreatedAt: &createdAt,
			Network: &runtimeapi.PodSandboxNetworkStatus{
				Ip: &FakePodSandboxIP,
			},
			Labels:      config.Labels,
			Annotations: config.Annotations,
		},
	}

	return &runtimeapi.RunPodSandboxResponse{PodSandboxId: &podSandboxID}, nil
}

func (r *FakeRuntimeServer) StopPodSandbox(ctx context.Context, in *runtimeapi.StopPodSandboxRequest) (*runtimeapi.StopPodSandboxResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "StopPodSandbox")

	podSandboxID := in.GetPodSandboxId()
	notReadyState := runtimeapi.PodSandboxState_SANDBOX_NOTREADY
	if s, ok := r.Sandboxes[podSandboxID]; ok {
		s.State = &notReadyState
	} else {
		return nil, fmt.Errorf("pod sandbox %s not found", podSandboxID)
	}

	return &runtimeapi.StopPodSandboxResponse{}, nil
}

func (r *FakeRuntimeServer) RemovePodSandbox(ctx context.Context, in *runtimeapi.RemovePodSandboxRequest) (*runtimeapi.RemovePodSandboxResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "RemovePodSandbox")

	// Remove the pod sandbox
	delete(r.Sandboxes, in.GetPodSandboxId())

	return &runtimeapi.RemovePodSandboxResponse{}, nil
}

func (r *FakeRuntimeServer) PodSandboxStatus(ctx context.Context, in *runtimeapi.PodSandboxStatusRequest) (*runtimeapi.PodSandboxStatusResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "PodSandboxStatus")

	podSandboxID := in.GetPodSandboxId()
	s, ok := r.Sandboxes[podSandboxID]
	if !ok {
		return nil, fmt.Errorf("pod sandbox %q not found", podSandboxID)
	}

	return &runtimeapi.PodSandboxStatusResponse{Status: &s.PodSandboxStatus}, nil
}

func (r *FakeRuntimeServer) ListPodSandbox(ctx context.Context, in *runtimeapi.ListPodSandboxRequest) (*runtimeapi.ListPodSandboxResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "ListPodSandbox")

	filter := in.GetFilter()
	result := make([]*runtimeapi.PodSandbox, 0)
	for id, s := range r.Sandboxes {
		if filter != nil {
			if filter.Id != nil && filter.GetId() != id {
				continue
			}
			if filter.State != nil && filter.GetState() != s.GetState() {
				continue
			}
			if filter.LabelSelector != nil && !filterInLabels(filter.LabelSelector, s.GetLabels()) {
				continue
			}
		}

		result = append(result, &runtimeapi.PodSandbox{
			Id:          s.Id,
			Metadata:    s.Metadata,
			State:       s.State,
			CreatedAt:   s.CreatedAt,
			Labels:      s.Labels,
			Annotations: s.Annotations,
		})
	}

	return &runtimeapi.ListPodSandboxResponse{Items: result}, nil
}

func (r *FakeRuntimeServer) CreateContainer(ctx context.Context, in *runtimeapi.CreateContainerRequest) (*runtimeapi.CreateContainerResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "CreateContainer")

	// ContainerID should be randomized for real container runtime, but here just use
	// fixed BuildContainerName() for easily making fake containers.
	podSandboxID := in.GetPodSandboxId()
	config := in.GetConfig()
	containerID := BuildContainerName(config.Metadata, podSandboxID)
	createdAt := time.Now().Unix()
	createdState := runtimeapi.ContainerState_CONTAINER_CREATED
	imageRef := config.Image.GetImage()
	r.Containers[containerID] = &FakeContainer{
		ContainerStatus: runtimeapi.ContainerStatus{
			Id:          &containerID,
			Metadata:    config.Metadata,
			Image:       config.Image,
			ImageRef:    &imageRef,
			CreatedAt:   &createdAt,
			State:       &createdState,
			Labels:      config.Labels,
			Annotations: config.Annotations,
		},
		SandboxID: podSandboxID,
	}

	return &runtimeapi.CreateContainerResponse{ContainerId: &containerID}, nil
}

func (r *FakeRuntimeServer) StartContainer(ctx context.Context, in *runtimeapi.StartContainerRequest) (*runtimeapi.StartContainerResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "StartContainer")

	containerID := in.GetContainerId()
	c, ok := r.Containers[containerID]
	if !ok {
		return nil, fmt.Errorf("container %s not found", containerID)
	}

	// Set container to running.
	startedAt := time.Now().Unix()
	runningState := runtimeapi.ContainerState_CONTAINER_RUNNING
	c.State = &runningState
	c.StartedAt = &startedAt

	return &runtimeapi.StartContainerResponse{}, nil
}

func (r *FakeRuntimeServer) StopContainer(ctx context.Context, in *runtimeapi.StopContainerRequest) (*runtimeapi.StopContainerResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "StopContainer")

	containerID := in.GetContainerId()
	c, ok := r.Containers[containerID]
	if !ok {
		return nil, fmt.Errorf("container %q not found", containerID)
	}

	// Set container to exited state.
	finishedAt := time.Now().Unix()
	exitedState := runtimeapi.ContainerState_CONTAINER_EXITED
	c.State = &exitedState
	c.FinishedAt = &finishedAt

	return &runtimeapi.StopContainerResponse{}, nil
}

func (r *FakeRuntimeServer) RemoveContainer(ctx context.Context, in *runtimeapi.RemoveContainerRequest) (*runtimeapi.RemoveContainerResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "RemoveContainer")

	// Remove the container
	delete(r.Containers, in.GetContainerId())

	return &runtimeapi.RemoveContainerResponse{}, nil
}

func (r *FakeRuntimeServer) ListContainers(ctx context.Context, in *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "ListContainers")

	filter := in.GetFilter()
	result := make([]*runtimeapi.Container, 0)
	for _, s := range r.Containers {
		if filter != nil {
			if filter.Id != nil && filter.GetId() != s.GetId() {
				continue
			}
			if filter.PodSandboxId != nil && filter.GetPodSandboxId() != s.SandboxID {
				continue
			}
			if filter.State != nil && filter.GetState() != s.GetState() {
				continue
			}
			if filter.LabelSelector != nil && !filterInLabels(filter.LabelSelector, s.GetLabels()) {
				continue
			}
		}

		result = append(result, &runtimeapi.Container{
			Id:           s.Id,
			CreatedAt:    s.CreatedAt,
			PodSandboxId: &s.SandboxID,
			Metadata:     s.Metadata,
			State:        s.State,
			Image:        s.Image,
			ImageRef:     s.ImageRef,
			Labels:       s.Labels,
			Annotations:  s.Annotations,
		})
	}

	return &runtimeapi.ListContainersResponse{Containers: result}, nil
}

func (r *FakeRuntimeServer) ContainerStatus(ctx context.Context, in *runtimeapi.ContainerStatusRequest) (*runtimeapi.ContainerStatusResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "ContainerStatus")

	containerID := in.GetContainerId()
	c, ok := r.Containers[containerID]
	if !ok {
		return nil, fmt.Errorf("container %q not found", containerID)
	}

	return &runtimeapi.ContainerStatusResponse{Status: &c.ContainerStatus}, nil
}

func (r *FakeRuntimeServer) ExecSync(ctx context.Context, in *runtimeapi.ExecSyncRequest) (*runtimeapi.ExecSyncResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "ExecSync")
	exitCode := int32(0)
	return &runtimeapi.ExecSyncResponse{Stdout: nil, Stderr: nil, ExitCode: &exitCode}, nil
}

func (r *FakeRuntimeServer) Exec(ctx context.Context, in *runtimeapi.ExecRequest) (*runtimeapi.ExecResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "Exec")
	return &runtimeapi.ExecResponse{}, nil
}

func (r *FakeRuntimeServer) Attach(ctx context.Context, in *runtimeapi.AttachRequest) (*runtimeapi.AttachResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "Attach")
	return &runtimeapi.AttachResponse{}, nil
}

func (r *FakeRuntimeServer) PortForward(ctx context.Context, in *runtimeapi.PortForwardRequest) (*runtimeapi.PortForwardResponse, error) {
	r.Lock()
	defer r.Unlock()

	r.Called = append(r.Called, "PortForward")
	return &runtimeapi.PortForwardResponse{}, nil
}

func (r *FakeRuntimeServer) UpdateRuntimeConfig(ctx context.Context, in *runtimeapi.UpdateRuntimeConfigRequest) (*runtimeapi.UpdateRuntimeConfigResponse, error) {
	return &runtimeapi.UpdateRuntimeConfigResponse{}, nil
}
