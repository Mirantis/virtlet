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

func TestSetImageFilepath(t *testing.T) {
	tests := []struct {
		name     string
		filepath string
		hash     string
	}{
		{
			name:     "my-favorite-distro",
			filepath: "/opt/data/test/my-favorite-distro.qcow2",
			hash:     "bd4d2bd18d926910af052843198b994f5f3769193368c834ae0ee3d6bdd4fe4e",
		},
		{
			name:     "another-distro",
			filepath: "/tmp/another-distro.qcow2",
			hash:     "c2b604cdcc4aac54961681f381ceafc074d6ece94602e49ae1cc3464c81563f4",
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.VerifyImagesSchema(); err != nil {
			t.Fatal(err)
		}

		err = b.SetImageFilepath(tc.name, tc.filepath)
		if err != nil {
			t.Fatal(err)
		}

		if err := b.db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("images"))
			if bucket == nil {
				return fmt.Errorf("Bucket 'images' doesn't exist")
			}

			filepath, err := getString(bucket, tc.hash)
			if err != nil {
				return err
			}

			if filepath != tc.filepath {
				t.Errorf("Expected %s, instead got %s", tc.filepath, filepath)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGetImageFilepath(t *testing.T) {
	tests := []struct {
		name     string
		filepath string
	}{
		{
			name:     "my-favorite-distro",
			filepath: "/opt/data/test/my-favorite-distro.qcow2",
		},
		{
			name:     "another-distro",
			filepath: "/tmp/another-distro.qcow2",
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.VerifyImagesSchema(); err != nil {
			t.Fatal(err)
		}

		err = b.SetImageFilepath(tc.name, tc.filepath)
		if err != nil {
			t.Fatal(err)
		}

		filepath, err := b.GetImageFilepath(tc.name)
		if err != nil {
			t.Fatal(err)
		}

		if filepath != tc.filepath {
			t.Errorf("Expected %s, instead got %s", tc.filepath, filepath)
		}
	}
}
