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

func getKey(name string) string {
	hash := sha256.New()
	hash.Write([]byte(name))
	sum := hex.EncodeToString(hash.Sum(nil))

	return sum
}

func (b *BoltClient) VerifyImagesSchema() error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("images"))
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) SetImageFilepath(name, filepath string) error {
	key := getKey(name)

	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("images"))
		if bucket == nil {
			return fmt.Errorf("Bucket 'images' doesn't exist")
		}

		if err := bucket.Put([]byte(key), []byte(filepath)); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) GetImageFilepath(name string) (string, error) {
	var filepath string

	key := getKey(name)

	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("images"))
		if bucket == nil {
			return fmt.Errorf("Bucket 'images' doesn't exist")
		}

		fp, err := getString(bucket, key)
		if err != nil {
			return err
		}
		filepath = fp

		return nil
	})

	return filepath, err
}
