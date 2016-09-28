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

	"github.com/boltdb/bolt"
)

func (b *BoltClient) SetLabels(containerId string, labels map[string]string) error {
	strLabels, err := json.Marshal(labels)
	if err != nil {
		return err
	}

	err = b.db.Batch(func(tx *bolt.Tx) error {
		parentBucket, err := tx.CreateBucketIfNotExists([]byte("virtualization"))
		if err != nil {
			return err
		}

		bucket, err := parentBucket.CreateBucketIfNotExists([]byte(containerId))
		if err != nil {
			return err
		}

		if err := bucket.Put([]byte("labels"), []byte(strLabels)); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) SetAnnotations(containerId string, annotations map[string]string) error {
	strAnnotations, err := json.Marshal(annotations)
	if err != nil {
		return err
	}

	err = b.db.Batch(func(tx *bolt.Tx) error {
		parentBucket, err := tx.CreateBucketIfNotExists([]byte("virtualization"))
		if err != nil {
			return err
		}

		bucket, err := parentBucket.CreateBucketIfNotExists([]byte(containerId))
		if err != nil {
			return err
		}

		if err := bucket.Put([]byte("annotations"), []byte(strAnnotations)); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) GetLabels(containerId string) (map[string]string, error) {
	var labels map[string]string

	err := b.db.View(func(tx *bolt.Tx) error {
		parentBucket := tx.Bucket([]byte("virtualization"))
		if parentBucket == nil {
			return fmt.Errorf("Bucket 'virtualization' doesn't exist")
		}

		bucket := parentBucket.Bucket([]byte(containerId))
		if bucket == nil {
			return fmt.Errorf("Bucket '%s' doesn't exist", containerId)
		}

		byteLabels, err := get(bucket, []byte("labels"))
		if err != nil {
			return err
		}

		if err := json.Unmarshal(byteLabels, &labels); err != nil {
			return err
		}

		return nil
	})

	return labels, err
}

func (b *BoltClient) GetAnnotations(containerId string) (map[string]string, error) {
	var annotations map[string]string

	err := b.db.View(func(tx *bolt.Tx) error {
		parentBucket := tx.Bucket([]byte("virtualization"))
		if parentBucket == nil {
			return fmt.Errorf("Bucket 'virtualization' doesn't exist")
		}

		bucket := parentBucket.Bucket([]byte(containerId))
		if b == nil {
			return fmt.Errorf("Bucket '%s' doesn't exist", containerId)
		}

		byteAnnotations, err := get(bucket, []byte("labels"))
		if err != nil {
			return err
		}

		if err := json.Unmarshal(byteAnnotations, &annotations); err != nil {
			return err
		}

		return nil
	})

	return annotations, err
}
