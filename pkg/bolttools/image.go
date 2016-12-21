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
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/boltdb/bolt"
)

const imageBucket = "images"

func getKey(name string) string {
	hash := sha256.New()
	hash.Write([]byte(name))
	return hex.EncodeToString(hash.Sum(nil))
}

func (b *BoltClient) EnsureImageSchema() error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(imageBucket)); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) SetImageName(volumeName, imageName string) error {
	key := getKey(volumeName)
	return b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(imageBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %q doesn't exist", imageBucket)
		}

		if err := bucket.Put([]byte(key), []byte(imageName)); err != nil {
			return err
		}

		return nil
	})
}

func (b *BoltClient) GetImageName(volumeName string) (string, error) {
	imageName := ""
	key := getKey(volumeName)
	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(imageBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %q doesn't exist", imageBucket)
		}

		fp := bucket.Get([]byte(key))
		if fp != nil {
			imageName = string(fp)
		}

		return nil
	})
	return imageName, err
}

func (b *BoltClient) RemoveImage(volumeName string) error {
	key := getKey(volumeName)
	return b.db.Batch(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(imageBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %q doesn't exist", imageBucket)
		}

		if err := bucket.Delete([]byte(key)); err != nil {
			return err
		}

		return nil
	})
}
