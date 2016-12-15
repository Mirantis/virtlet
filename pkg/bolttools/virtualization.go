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

package bolttools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type ContainerInfo struct {
	CreatedAt             int64
	StartedAt             int64
	SandboxId             string
	Image                 string
	RootImageSnapshotName string
	Labels                map[string]string
	Annotations           map[string]string
	State                 kubeapi.ContainerState
}

func (b *BoltClient) VerifyVirtualizationSchema() error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte("virtualization")); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) SetContainer(containerId, sandboxId, image, rootImageSnapshotName string, labels, annotations map[string]string) error {
	strLabels, err := json.Marshal(labels)
	if err != nil {
		return err
	}

	strAnnotations, err := json.Marshal(annotations)
	if err != nil {
		return err
	}

	err = b.db.Update(func(tx *bolt.Tx) error {
		parentBucket := tx.Bucket([]byte("virtualization"))
		if parentBucket == nil {
			return fmt.Errorf("bucket 'virtualization' doesn't exist")
		}

		bucket, err := parentBucket.CreateBucketIfNotExists([]byte(containerId))
		if err != nil {
			return err
		}

		if err := bucket.Put([]byte("createdAt"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10))); err != nil {
			return err
		}

		if err := bucket.Put([]byte("sandboxId"), []byte(sandboxId)); err != nil {
			return err
		}

		// Add container id to corresponding Sandbox Container's ids list as well
		sandboxBucket := tx.Bucket([]byte("sandbox"))
		if sandboxBucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		sandboxIDBucket := sandboxBucket.Bucket([]byte(sandboxId))
		if sandboxIDBucket == nil {
			return fmt.Errorf("Sandbox bucket '%s' doesn't exist", sandboxId)
		}

		if err := sandboxIDBucket.Put([]byte("ContainerID"), []byte(containerId)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("image"), []byte(image)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("rootImageSnapshotName"), []byte(rootImageSnapshotName)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("labels"), []byte(strLabels)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("annotations"), []byte(strAnnotations)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("state"), []byte{byte(kubeapi.ContainerState_CREATED)}); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) UpdateStartedAt(containerId string, startedAt string) error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		parentBucket := tx.Bucket([]byte("virtualization"))
		if parentBucket == nil {
			return fmt.Errorf("bucket 'virtualization' doesn't exist")
		}

		bucket := parentBucket.Bucket([]byte(containerId))
		if bucket == nil {
			// Container info removed, but sandbox still exists
			return nil
		}

		if err := bucket.Put([]byte("startedAt"), []byte(startedAt)); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) UpdateState(containerId string, state byte) error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		parentBucket := tx.Bucket([]byte("virtualization"))
		if parentBucket == nil {
			return fmt.Errorf("bucket 'virtualization' doesn't exist")
		}

		bucket := parentBucket.Bucket([]byte(containerId))
		if bucket == nil {
			// Container info removed, but sandbox still exists
			return nil
		}

		if err := bucket.Put([]byte("state"), []byte{state}); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) GetContainerInfo(containerId string) (*ContainerInfo, error) {
	var containerInfo *ContainerInfo

	if err := b.db.View(func(tx *bolt.Tx) error {
		parentBucket := tx.Bucket([]byte("virtualization"))
		if parentBucket == nil {
			return fmt.Errorf("bucket 'virtualization' doesn't exist")
		}

		bucket := parentBucket.Bucket([]byte(containerId))
		if bucket == nil {
			// Container info removed, but sandbox still exists
			return nil
		}

		strCreatedAt, err := getString(bucket, "createdAt")
		if err != nil {
			return err
		}

		createdAt, err := strconv.ParseInt(strCreatedAt, 10, 64)
		if err != nil {
			return err
		}

		var startedAt int64
		bytesStartedAt := bucket.Get([]byte("startedAt"))
		if bytesStartedAt != nil {
			startedAt, err = strconv.ParseInt(string(bytesStartedAt), 10, 64)
			if err != nil {
				return err
			}
		} else {
			startedAt = 0
		}

		sandboxId, err := getString(bucket, "sandboxId")
		if err != nil {
			return err
		}

		rootImageSnapshotName, err := getString(bucket, "rootImageSnapshotName")
		if err != nil {
			return err
		}

		image, err := getString(bucket, "image")
		if err != nil {
			return err
		}

		byteLabels, err := get(bucket, []byte("labels"))
		if err != nil {
			return err
		}

		var labels map[string]string
		if err := json.Unmarshal(byteLabels, &labels); err != nil {
			return err
		}

		byteAnnotations, err := get(bucket, []byte("annotations"))
		if err != nil {
			return err
		}

		var annotations map[string]string
		if err := json.Unmarshal(byteAnnotations, &annotations); err != nil {
			return err
		}

		byteState, err := get(bucket, []byte("state"))
		if err != nil {
			return err
		}

		containerInfo = &ContainerInfo{
			CreatedAt: createdAt,
			StartedAt: startedAt,
			SandboxId: sandboxId,
			Image:     image,
			RootImageSnapshotName: rootImageSnapshotName,
			Labels:                labels,
			Annotations:           annotations,
			State:                 kubeapi.ContainerState(byteState[0]),
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return containerInfo, nil
}

func (b *BoltClient) RemoveContainer(containerId string) error {
	return b.db.Batch(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("virtualization"))
		if bucket == nil {
			return fmt.Errorf("bucket 'virtualization' doesn't exist")
		}

		// Delete container id from corresponding Sandbox Container's ids list as well
		containerBucket := bucket.Bucket([]byte(containerId))
		if containerBucket == nil {
			return fmt.Errorf("Container bucket '%s' doesn't exist", containerId)
		}

		sandboxId, err := getString(containerBucket, "sandboxId")
		if err != nil {
			return err
		}

		sandboxBucket := tx.Bucket([]byte("sandbox"))
		if sandboxBucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		sandboxIDBucket := sandboxBucket.Bucket([]byte(sandboxId))
		if sandboxIDBucket == nil {
			return fmt.Errorf("Sandbox bucket '%s' doesn't exist", sandboxId)
		}

		if err := sandboxIDBucket.Put([]byte("ContainerID"), []byte("")); err != nil {
			return err
		}

		if err := bucket.DeleteBucket([]byte(containerId)); err != nil {
			return err
		}

		return nil
	})
}
