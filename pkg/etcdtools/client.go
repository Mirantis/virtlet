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

package etcdtools

import (
	"time"

	etcd "github.com/coreos/etcd/client"
)

type KeysAPITool struct {
	KeysAPI etcd.KeysAPI
}

func NewKeysAPI(endpoints []string) (*KeysAPITool, error) {
	cfg := etcd.Config{
		Endpoints:               endpoints,
		Transport:               etcd.DefaultTransport,
		HeaderTimeoutPerRequest: time.Second,
	}
	c, err := etcd.New(cfg)
	if err != nil {
		return nil, err
	}
	kapi := etcd.NewKeysAPI(c)

	return &KeysAPITool{KeysAPI: kapi}, nil
}
