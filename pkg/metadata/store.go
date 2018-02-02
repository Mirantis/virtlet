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
	"encoding/json"
	"fmt"
	"io"

	"github.com/jonboulle/clockwork"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/network"
)

// PodSandboxInfo contains metadata information about pod sandbox instance
type PodSandboxInfo struct {
	podID string

	Metadata             *kubeapi.PodSandboxMetadata
	CreatedAt            int64
	Labels               map[string]string
	Hostname             string
	LogDirectory         string
	Annotations          map[string]string
	State                kubeapi.PodSandboxState
	CgroupParent         string
	NamespaceOption      *kubeapi.NamespaceOption
	ContainerSideNetwork *network.ContainerSideNetwork
}

// AsPodSandboxStatus converts PodSandboxInfo to an instance of PodSandboxStatus
func (psi *PodSandboxInfo) AsPodSandboxStatus() *kubeapi.PodSandboxStatus {
	return &kubeapi.PodSandboxStatus{
		Id:        psi.podID,
		Metadata:  psi.Metadata,
		State:     psi.State,
		CreatedAt: psi.CreatedAt,
		Linux: &kubeapi.LinuxPodSandboxStatus{
			Namespaces: &kubeapi.Namespace{
				Options: psi.NamespaceOption,
			},
		},
		Labels:      psi.Labels,
		Annotations: psi.Annotations,
	}
}

// AsPodSandbox converts PodSandboxInfo to an instance of PodSandbox
func (psi *PodSandboxInfo) AsPodSandbox() *kubeapi.PodSandbox {
	return &kubeapi.PodSandbox{
		Id:        psi.podID,
		Metadata:  psi.Metadata,
		State:     psi.State,
		CreatedAt: psi.CreatedAt,
		Labels:    psi.Labels,
	}
}

// PodSandboxMetadata contains methods of a single Pod sandbox
type PodSandboxMetadata interface {
	// GetID returns ID of the pod sandbox managed by this object
	GetID() string

	// Retrieve loads from DB and returns pod sandbox data bound to the object
	Retrieve() (*PodSandboxInfo, error)

	// Save allows to create/modify/delete pod sandbox instance bound to the object.
	// Supplied handler gets current PodSandboxInfo value (nil if doesn't exist) and returns new structure
	// value to be saved or nil to delete. If error value is returned from the handler, the transaction is
	// rolled back and returned error becomes the result of the function
	Save(func(*PodSandboxInfo) (*PodSandboxInfo, error)) error
}

// SandboxMetadataStore contains methods to operate on Pod sandboxes
type SandboxMetadataStore interface {
	// PodSandbox returns interface instance which manages pod sandbox with given ID
	PodSandbox(podID string) PodSandboxMetadata

	// ListPodSandboxes returns list of pod sandboxes that match given filter
	ListPodSandboxes(filter *kubeapi.PodSandboxFilter) ([]PodSandboxMetadata, error)
}

// ContainerInfo contains metadata information about container instance
type ContainerInfo struct {
	Name                string
	CreatedAt           int64
	StartedAt           int64
	SandboxID           string
	Image               string
	RootImageVolumeName string
	Labels              map[string]string
	Annotations         map[string]string
	Attempt             uint32
	State               kubeapi.ContainerState
}

// ContainerMetadata contains methods of a single container (VM)
type ContainerMetadata interface {
	// GetID returns ID of the container managed by this object
	GetID() string

	// Retrieve loads from DB and returns container data bound to the object
	Retrieve() (*ContainerInfo, error)

	// Save allows to create/modify/delete container data bound to the object.
	// Supplied handler gets current ContainerInfo value (nil if doesn't exist) and returns new structure
	// value to be saved or nil to delete. If error value is returned from the handler, the transaction is
	// rolled back and returned error becomes the result of the function
	Save(func(*ContainerInfo) (*ContainerInfo, error)) error
}

// ContainerMetadataStore contains methods to operate on containers (VMs)
type ContainerMetadataStore interface {
	// Container returns interface instance which manages container with given ID
	Container(containerID string) ContainerMetadata

	// ListPodContainers returns a list of containers that belong to the pod with given ID value
	ListPodContainers(podID string) ([]ContainerMetadata, error)

	// ImagesInUse returns a set of images in use by containers in the store.
	// The keys of the returned map are image names and the values are always true.
	ImagesInUse() (map[string]bool, error)
}

// MetadataStore provides single interface for metadata storage implementation
type MetadataStore interface {
	SandboxMetadataStore
	ContainerMetadataStore
	io.Closer
}

// NewPodSandboxInfo is a factory function for PodSandboxInfo instances
func NewPodSandboxInfo(config *kubeapi.PodSandboxConfig, csnData interface{}, state kubeapi.PodSandboxState, clock clockwork.Clock) (*PodSandboxInfo, error) {
	linuxSandbox := config.Linux
	if linuxSandbox == nil {
		linuxSandbox = &kubeapi.LinuxPodSandboxConfig{}
	}
	namespaceOptions := &kubeapi.NamespaceOption{}
	if linuxSandbox.SecurityContext != nil && linuxSandbox.SecurityContext.NamespaceOptions != nil {
		namespaceOptions = linuxSandbox.SecurityContext.NamespaceOptions
	}

	var csn *network.ContainerSideNetwork
	switch csnData.(type) {
	case string:
		data := csnData.(string)
		if len(data) > 0 {
			if err := json.Unmarshal([]byte(data), &csn); err != nil {
				return nil, err
			}
		}
	case []byte:
		data := csnData.([]byte)
		if len(data) > 0 {
			if err := json.Unmarshal((data), &csn); err != nil {
				return nil, err
			}
		}
	case *network.ContainerSideNetwork:
		csn = csnData.(*network.ContainerSideNetwork)
	case nil:
		break
	default:
		return nil, fmt.Errorf("CSN data in unknown format: %v", csnData)
	}

	return &PodSandboxInfo{
		Metadata:             config.Metadata,
		CreatedAt:            clock.Now().UnixNano(),
		Labels:               config.Labels,
		Hostname:             config.Hostname,
		LogDirectory:         config.LogDirectory,
		Annotations:          config.Annotations,
		State:                state,
		CgroupParent:         linuxSandbox.CgroupParent,
		NamespaceOption:      namespaceOptions,
		ContainerSideNetwork: csn,
	}, nil
}
