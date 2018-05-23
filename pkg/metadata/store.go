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

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/network"
)

// PodSandboxMetadata contains methods of a single Pod sandbox
type PodSandboxMetadata interface {
	// GetID returns ID of the pod sandbox managed by this object
	GetID() string

	// Retrieve loads from DB and returns pod sandbox data bound to the object
	Retrieve() (*types.PodSandboxInfo, error)

	// Save allows to create/modify/delete pod sandbox instance bound to the object.
	// Supplied handler gets current PodSandboxInfo value (nil if doesn't exist) and returns new structure
	// value to be saved or nil to delete. If error value is returned from the handler, the transaction is
	// rolled back and returned error becomes the result of the function
	Save(func(*types.PodSandboxInfo) (*types.PodSandboxInfo, error)) error
}

// SandboxStore contains methods to operate on Pod sandboxes
type SandboxStore interface {
	// PodSandbox returns interface instance which manages pod sandbox with given ID
	PodSandbox(podID string) PodSandboxMetadata

	// ListPodSandboxes returns list of pod sandboxes that match given filter
	ListPodSandboxes(filter *types.PodSandboxFilter) ([]PodSandboxMetadata, error)
}

// ContainerMetadata contains methods of a single container (VM)
type ContainerMetadata interface {
	// GetID returns ID of the container managed by this object
	GetID() string

	// Retrieve loads from DB and returns container data bound to the object
	Retrieve() (*types.ContainerInfo, error)

	// Save allows to create/modify/delete container data bound to the object.
	// Supplied handler gets current ContainerInfo value (nil if doesn't exist) and returns new structure
	// value to be saved or nil to delete. If error value is returned from the handler, the transaction is
	// rolled back and returned error becomes the result of the function
	Save(func(*types.ContainerInfo) (*types.ContainerInfo, error)) error
}

// ContainerStore contains methods to operate on containers (VMs)
type ContainerStore interface {
	// Container returns interface instance which manages container with given ID
	Container(containerID string) ContainerMetadata

	// ListPodContainers returns a list of containers that belong to the pod with given ID value
	ListPodContainers(podID string) ([]ContainerMetadata, error)

	// ImagesInUse returns a set of images in use by containers in the store.
	// The keys of the returned map are image names and the values are always true.
	ImagesInUse() (map[string]bool, error)
}

// Store provides single interface for metadata storage implementation
type Store interface {
	SandboxStore
	ContainerStore
	io.Closer
}

// NewPodSandboxInfo is a factory function for PodSandboxInfo instances
func NewPodSandboxInfo(config *types.PodSandboxConfig, csnData interface{}, state types.PodSandboxState, clock clockwork.Clock) (*types.PodSandboxInfo, error) {
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

	return &types.PodSandboxInfo{
		Config:               config,
		CreatedAt:            clock.Now().UnixNano(),
		State:                state,
		ContainerSideNetwork: csn,
	}, nil
}
