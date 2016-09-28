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
	"reflect"
	"testing"

	"github.com/boltdb/bolt"
)

func TestSetLabels(t *testing.T) {
	tests := []struct {
		containerId  string
		labels       map[string]string
		expectedJson string
	}{
		{
			containerId: "571454b9-89d4-4a32-bc2c-9c0c44492b3c",
			labels: map[string]string{
				"foo":  "bar",
				"fizz": "buzz",
			},
			expectedJson: "{\"fizz\":\"buzz\",\"foo\":\"bar\"}",
		},
		{
			containerId:  "8808803f-2957-43f7-a5d1-4b9bf0d9f20c",
			labels:       nil,
			expectedJson: "null",
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		err = b.SetLabels(tc.containerId, tc.labels)
		if err != nil {
			t.Fatal(err)
		}

		if err := b.db.View(func(tx *bolt.Tx) error {
			parentBucket := tx.Bucket([]byte("virtualization"))
			if parentBucket == nil {
				return fmt.Errorf("Bucket 'virtualization' doesn't exist")
			}

			bucket := parentBucket.Bucket([]byte(tc.containerId))
			if bucket == nil {
				return fmt.Errorf("Bucket '%s' doesn't exist", tc.containerId)
			}

			labels, err := getString(bucket, "labels")
			if err != nil {
				return err
			}

			if labels != tc.expectedJson {
				t.Errorf("Expected %s, instead got %s", tc.expectedJson, labels)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGetLabels(t *testing.T) {
	tests := []struct {
		containerId string
		labels      map[string]string
	}{
		{
			containerId: "6c566b6d-73ed-4a45-b09b-abe89d116174",
			labels: map[string]string{
				"fizz": "buzz",
				"virt": "let",
			},
		},
		{
			containerId: "802d756b-efab-4a06-9f7a-a3a207618e5c",
			labels:      nil,
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		err = b.SetLabels(tc.containerId, tc.labels)
		if err != nil {
			t.Fatal(err)
		}

		labels, err := b.GetLabels(tc.containerId)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(labels, tc.labels) {
			t.Errorf("Expected %#v, instead got %#v", tc.labels, labels)
		}
	}
}
