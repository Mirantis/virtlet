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
	"github.com/boltdb/bolt"
)

type BoltClient struct {
	db *bolt.DB
}

func NewBoltClient() (*BoltClient, error) {
	db, err := bolt.Open("/var/data/virtlet/virtlet.db", 0600, nil)
	if err != nil {
		return nil, err
	}

	client := &BoltClient{db: db}
	if err := client.VerifyImagesSchema(); err != nil {
		return nil, err
	}
	if err := client.VerifySandboxSchema(); err != nil {
		return nil, err
	}
	if err := client.VerifyVirtualizationSchema(); err != nil {
		return nil, err
	}

	return client, nil
}

func (b *BoltClient) Close() error {
	return b.db.Close()
}
