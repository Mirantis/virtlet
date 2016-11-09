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
	"fmt"
	"testing"

	"github.com/boltdb/bolt"
)

func TestGet(t *testing.T) {
	tests := []struct {
		bucketName  []byte
		valuesToSet map[string]string
		valuesToGet []string
		error       bool
	}{
		{
			bucketName: []byte("foo-bucket"),
			valuesToSet: map[string]string{
				"foo":  "bar",
				"fizz": "buzz",
			},
			valuesToGet: []string{"foo", "fizz"},
			error:       false,
		},
		{
			bucketName: []byte("fizz-bucket"),
			valuesToSet: map[string]string{
				"fizz": "buzz",
				"virt": "let",
			},
			valuesToGet: []string{"some", "non-existing", "keys"},
			error:       true,
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.db.Batch(func(tx *bolt.Tx) error {
			bucket, err := tx.CreateBucketIfNotExists(tc.bucketName)
			if err != nil {
				return err
			}

			for k, v := range tc.valuesToSet {
				if err := bucket.Put([]byte(k), []byte(v)); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		if err := b.db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket(tc.bucketName)
			if bucket == nil {
				return fmt.Errorf("bucket '%s' doesn't exist", tc.bucketName)
			}

			for _, k := range tc.valuesToGet {
				// Test get function
				v, err := get(bucket, []byte(k))
				if err != nil {
					if tc.error {
						continue
					}
					return err
				}

				if string(v) != tc.valuesToSet[k] {
					t.Errorf("Expected %s, instead got %s", tc.valuesToSet[k], v)
				}

				// Test getString function
				vs, err := getString(bucket, k)
				if err != nil {
					if tc.error {
						continue
					}
					return err
				}

				if vs != tc.valuesToSet[k] {
					t.Errorf("Expected %s, instead got %s", tc.valuesToSet[k], vs)
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
