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

package metadata

import (
	"github.com/boltdb/bolt"
)

type boltClient struct {
	db *bolt.DB
}

// NewMetadataStore is a factory function for MetadataStore interface
func NewMetadataStore(path string) (MetadataStore, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}

	client := &boltClient{db: db}
	return client, nil
}

// Close releases all database resources
func (b boltClient) Close() error {
	return b.db.Close()
}
