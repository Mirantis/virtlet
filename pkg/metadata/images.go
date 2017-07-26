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
	"github.com/boltdb/bolt"
)

var imageBucket = []byte("images")

// SetImageName associates image name with the volume
func (b *boltClient) SetImageName(volumeName, imageName string) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(imageBucket)
		if err != nil {
			return err
		}

		return bucket.Put([]byte(volumeName), []byte(imageName))
	})
}

// GetImageName returns image name associated with the volume
func (b *boltClient) GetImageName(volumeName string) (string, error) {
	imageName := ""
	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(imageBucket)
		if bucket == nil {
			return nil
		}

		fp := bucket.Get([]byte(volumeName))
		if fp != nil {
			imageName = string(fp)
		}

		return nil
	})
	return imageName, err
}

// RemoveImage removes volume name association from the volume name
func (b *boltClient) RemoveImage(volumeName string) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(imageBucket)
		if bucket == nil {
			return nil
		}

		return bucket.Delete([]byte(volumeName))
	})
}
