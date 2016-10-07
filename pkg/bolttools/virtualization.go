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
)

type ContainerInfo struct {
	CreatedAt   int64
	SandboxId   string
	Image       string
	Labels      map[string]string
	Annotations map[string]string
}

func (b *BoltClient) SetContainer(containerId, sandboxId, image string, labels, annotations map[string]string) error {
	strLabels, err := json.Marshal(labels)
	if err != nil {
		return err
	}

	strAnnotations, err := json.Marshal(annotations)
	if err != nil {
		return err
	}

	err = b.db.Update(func(tx *bolt.Tx) error {
		parentBucket, err := tx.CreateBucketIfNotExists([]byte("virtualization"))
		if err != nil {
			return err
		}

		bucket, err := parentBucket.CreateBucketIfNotExists([]byte(containerId))
		if err != nil {
			return err
		}

		if err := bucket.Put([]byte("createdAt"), []byte(strconv.FormatInt(time.Now().Unix(), 10))); err != nil {
			return err
		}

		if err := bucket.Put([]byte("sandboxId"), []byte(sandboxId)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("image"), []byte(image)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("labels"), []byte(strLabels)); err != nil {
			return err
		}

		if err := bucket.Put([]byte("annotations"), []byte(strAnnotations)); err != nil {
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
			return fmt.Errorf("Bucket 'virtualization' doesn't exist")
		}

		bucket := parentBucket.Bucket([]byte(containerId))
		if bucket == nil {
			return fmt.Errorf("Bucket '%s' doesn't exist", containerId)
		}

		strCreatedAt, err := getString(bucket, "createdAt")
		if err != nil {
			return err
		}

		createdAt, err := strconv.ParseInt(strCreatedAt, 10, 64)
		if err != nil {
			return err
		}

		sandboxId, err := getString(bucket, "sandboxId")
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

		containerInfo = &ContainerInfo{
			CreatedAt:   createdAt,
			SandboxId:   sandboxId,
			Image:       image,
			Labels:      labels,
			Annotations: annotations,
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return containerInfo, nil
}
