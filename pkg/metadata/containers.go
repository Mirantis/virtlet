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

	"github.com/boltdb/bolt"
)

type containerMeta struct {
	client *boltClient
	id     string
}

// GetID returns ID of the container managed by this object
func (m containerMeta) GetID() string {
	return m.id
}

// Retrieve loads from DB and returns container data bound to the object
func (m containerMeta) Retrieve() (*ContainerInfo, error) {
	if m.GetID() == "" {
		return nil, errors.New("Container ID cannot be empty")
	}
	var ci *ContainerInfo
	err := m.client.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("containers"))
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
func (m containerMeta) Save(updater func(*ContainerInfo) (*ContainerInfo, error)) error {
	if m.GetID() == "" {
		return errors.New("Container ID cannot be empty")
	}
	return m.client.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("containers"))
		if err != nil {
			return err
		}
		var current *ContainerInfo
		var oldPodID string
		data := bucket.Get([]byte(m.GetID()))
		if data != nil {
			if err = json.Unmarshal(data, &current); err != nil {
				return err
			}
			oldPodID = current.SandboxID
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
				if err = removeContainerFrmSandbox(tx, m.GetID(), oldPodID); err != nil {
					return err
				}
			}
			return bucket.Delete([]byte(m.GetID()))
		}
		data, err = json.Marshal(newData)
		if err != nil {
			return err
		}

		if oldPodID != newData.SandboxID {
			if oldPodID != "" {
				if err = removeContainerFrmSandbox(tx, m.GetID(), oldPodID); err != nil {
					return err
				}
			}
			if newData.SandboxID != "" {
				if err = addContainerToSandbox(tx, m.GetID(), newData.SandboxID); err != nil {
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
	return bucket.Put([]byte("containers/"+containerID), []byte{})
}

func removeContainerFrmSandbox(tx *bolt.Tx, containerID, sandboxID string) error {
	bucket, err := getSandboxBucket(tx, sandboxID, false, true)
	if err != nil {
		return err
	}
	if bucket == nil {
		return nil
	}
	return bucket.Delete([]byte("containers/" + containerID))
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
		prefix := []byte("containers/")
		c := bucket.Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			result = append(result, b.Container(string(k[11:])))
		}
		return nil
	})
	return result, err
}
