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

package metadata

import (
	"time"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

// ContainerInfo contains metadata informations about container instance
type ContainerInfo struct {
	Name                string
	CreatedAt           int64
	StartedAt           int64
	SandboxId           string
	Image               string
	RootImageVolumeName string
	Labels              map[string]string
	Annotations         map[string]string
	SandBoxAnnotations  map[string]string
	NocloudFile         string
	State               kubeapi.ContainerState
}

// ImageMetadataStore contains methods to operate on VM images
type ImageMetadataStore interface {
	SetImageName(volumeName, imageName string) error
	GetImageName(volumeName string) (string, error)
	RemoveImage(volumeName string) error
}

// SandboxMetadataStore contains methods to operate on POD sandboxes
type SandboxMetadataStore interface {
	SetPodSandbox(config *kubeapi.PodSandboxConfig, networkConfiguration []byte, timeFunc func() time.Time) error
	UpdatePodState(podId string, state byte) error
	RemovePodSandbox(podId string) error
	GetPodSandboxContainerID(podId string) (string, error)
	GetPodSandboxAnnotations(podId string) (map[string]string, error)
	GetPodSandboxStatus(podId string) (*kubeapi.PodSandboxStatus, error)
	ListPodSandbox(filter *kubeapi.PodSandboxFilter) ([]*kubeapi.PodSandbox, error)
	GetPodNetworkConfigurationAsBytes(podId string) ([]byte, error)
	GetPodSandboxNameAndNamespace(podId string) (string, string, error)
}

// ContainerMetadataStore contains methods to operate on containers (VMs)
type ContainerMetadataStore interface {
	SetContainer(name, containerId, sandboxId, image, rootImageVolumeName string, labels, annotations map[string]string, nocloudFile string, timeFunc func() time.Time) error
	UpdateStartedAt(containerId string, startedAt string) error
	UpdateState(containerId string, state byte) error
	GetContainerInfo(containerId string) (*ContainerInfo, error)
	RemoveContainer(containerId string) error
}

// MetadataStore provides single interface for metadata storage implementation
type MetadataStore interface {
	ImageMetadataStore
	SandboxMetadataStore
	ContainerMetadataStore
}
