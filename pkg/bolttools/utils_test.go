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

	"github.com/Mirantis/virtlet/tests/criapi"
	"github.com/boltdb/bolt"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
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

func dumpDB(t *testing.T, db *bolt.DB) error {
	t.Log("=======Start DB content")
	err := db.View(func(tx *bolt.Tx) error {
		var iterateOverElements func(tx *bolt.Tx, bucket *bolt.Bucket, indent string)
		iterateOverElements = func(tx *bolt.Tx, bucket *bolt.Bucket, indent string) {
			var c *bolt.Cursor
			if bucket == nil {
				c = tx.Cursor()
			} else {
				c = bucket.Cursor()
			}
			for k, v := c.First(); k != nil; k, v = c.Next() {
				if v == nil {
					// SubBucket
					t.Logf(" %s BUCKET: %s", indent, string(k))
					if bucket == nil {
						//root bucket
						iterateOverElements(tx, tx.Bucket(k), "  "+indent)
					} else {
						iterateOverElements(tx, bucket.Bucket(k), "  "+indent)
					}
				} else {
					t.Logf(" %s %s: %s\n", indent, string(k), string(v))
				}
			}
		}
		iterateOverElements(tx, nil, "|_")
		return nil
	})
	t.Log("=======End DB content")

	return err
}

func SetUpBolt(t *testing.T, sandboxConfigs []*kubeapi.PodSandboxConfig, containerConfigs []*criapi.ContainerTestConfig) *BoltClient {
	b, err := NewFakeBoltClient()
	if err != nil {
		t.Fatal(err)
	}
	//Check filter on empty DB
	sandboxList, err := b.ListPodSandbox(&kubeapi.PodSandboxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if sandboxList == nil || len(sandboxList) != 0 {
		t.Errorf("Expected to recieve array of zero lenght as a result of list request against empty Bolt db.")
	}

	if err := b.EnsureSandboxSchema(); err != nil {
		t.Fatal(err)
	}

	for _, sandbox := range sandboxConfigs {
		if err := b.SetPodSandbox(sandbox, []byte{}); err != nil {
			t.Fatal(err)
		}
	}

	if err := b.EnsureVirtualizationSchema(); err != nil {
		t.Fatal(err)
	}

	for _, container := range containerConfigs {
		if err := b.SetContainer(container.Name, container.ContainerId, container.SandboxId, container.Image, container.RootImageSnapshotName, container.Labels, container.Annotations); err != nil {
			t.Fatal(err)
		}
	}

	dumpDB(t, b.db)

	return b
}
