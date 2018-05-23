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
*/

package metadata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/boltdb/bolt"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

var (
	containersBucket   = []byte("containers")
	containerKeyPrefix = []byte("containers/")
)

func containerKey(containerID string) []byte {
	return append(containerKeyPrefix, []byte(containerID)...)
}

type containerMeta struct {
	client *boltClient
	id     string
}

// GetID returns ID of the container managed by this object
func (m containerMeta) GetID() string {
	return m.id
}

// Retrieve loads from DB and returns container data bound to the object
func (m containerMeta) Retrieve() (*types.ContainerInfo, error) {
	if m.GetID() == "" {
		return nil, errors.New("Container ID cannot be empty")
	}
	var ci *types.ContainerInfo
	err := m.client.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(containersBucket)
		if bucket == nil {
			return nil
		}
		data := bucket.Get([]byte(m.GetID()))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &ci)
	})
	return ci, err
}

// Save allows to create/modify/delete container data bound to the object.
// Supplied handler gets current ContainerInfo value (nil if doesn't exist) and returns new structure
// value to be saved or nil to delete. If error value is returned from the handler, the transaction is
// rolled back and returned error becomes the result of the function
func (m containerMeta) Save(updater func(*types.ContainerInfo) (*types.ContainerInfo, error)) error {
	if m.GetID() == "" {
		return errors.New("Container ID cannot be empty")
	}
	return m.client.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(containersBucket)
		if err != nil {
			return err
		}
		var current *types.ContainerInfo
		var oldPodID string
		data := bucket.Get([]byte(m.GetID()))
		if data != nil {
			if err = json.Unmarshal(data, &current); err != nil {
				return err
			}
			oldPodID = current.Config.PodSandboxID
		}
		newData, err := updater(current)
		if err != nil {
			return err
		}

		if current == nil && newData == nil {
			return nil
		}

		if newData == nil {
			if oldPodID != "" {
				if err = removeContainerFromSandbox(tx, m.GetID(), oldPodID); err != nil {
					return err
				}
			}
			return bucket.Delete([]byte(m.GetID()))
		}
		newData.Id = m.GetID()
		data, err = json.Marshal(newData)
		if err != nil {
			return err
		}

		if oldPodID != newData.Config.PodSandboxID {
			if oldPodID != "" {
				if err = removeContainerFromSandbox(tx, m.GetID(), oldPodID); err != nil {
					return err
				}
			}
			if newData.Config.PodSandboxID != "" {
				if err = addContainerToSandbox(tx, m.GetID(), newData.Config.PodSandboxID); err != nil {
					return err
				}
			}
		}
		return bucket.Put([]byte(m.GetID()), data)
	})
}

func addContainerToSandbox(tx *bolt.Tx, containerID, sandboxID string) error {
	bucket, err := getSandboxBucket(tx, sandboxID, true, false)
	if err != nil {
		return err
	}
	return bucket.Put(containerKey(containerID), []byte{})
}

func removeContainerFromSandbox(tx *bolt.Tx, containerID, sandboxID string) error {
	bucket, err := getSandboxBucket(tx, sandboxID, false, true)
	if err != nil {
		return err
	}
	if bucket == nil {
		return nil
	}
	return bucket.Delete(containerKey(containerID))
}

// Container returns interface instance which manages container with given ID
func (b *boltClient) Container(containerID string) ContainerMetadata {
	return &containerMeta{id: containerID, client: b}
}

// ListPodContainers returns a list of containers that belong to the pod with given ID value
func (b *boltClient) ListPodContainers(podID string) ([]ContainerMetadata, error) {
	if podID == "" {
		return nil, errors.New("Pod sandbox ID cannot be empty")
	}
	var result []ContainerMetadata
	err := b.db.View(func(tx *bolt.Tx) error {
		bucket, err := getSandboxBucket(tx, podID, false, false)
		if err != nil {
			return err
		}
		c := bucket.Cursor()
		for k, _ := c.Seek(containerKeyPrefix); k != nil && bytes.HasPrefix(k, containerKeyPrefix); k, _ = c.Next() {
			result = append(result, b.Container(string(k[len(containerKeyPrefix):])))
		}
		return nil
	})
	return result, err
}

// ImagesInUse returns a set of images in use by containers in the store.
// The keys of the returned map are image names and the values are always true.
func (b *boltClient) ImagesInUse() (map[string]bool, error) {
	result := make(map[string]bool)
	if err := b.db.View(func(tx *bolt.Tx) error {
		c := tx.Cursor()
		for k, _ := c.Seek(sandboxKeyPrefix); k != nil && bytes.HasPrefix(k, sandboxKeyPrefix); k, _ = c.Next() {
			containers, err := b.ListPodContainers(string(k[len(sandboxKeyPrefix):]))
			if err != nil {
				return err
			}
			for _, containerMeta := range containers {
				ci, err := containerMeta.Retrieve()
				if err != nil {
					return err
				}
				if ci == nil {
					return fmt.Errorf("containerInfo of container %q not found in Virtlet metadata store", containerMeta.GetID())
				}
				result[ci.Config.Image] = true
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}
