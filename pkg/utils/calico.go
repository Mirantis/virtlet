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

package utils

import (
	"fmt"
	"os"

	"github.com/tigera/libcalico-go/lib/client"
)

type CalicoClient struct {
	client client.Client
}

func NewCalicoClient(etcdEndpoints string) (*CalicoClient, error) {
	if etcdEndpoints != "" {
		if err := os.Setenv("ETCD_ENDPOINTS", etcdEndpoints); err != nil {
			return nil, err
		}
	}

	// load client config from environment
	clientConfig, err := client.LoadClientConfig("")
	if err != nil {
		return nil, err
	}

	client, err := client.New(*clientConfig)
	if err != nil {
		return nil, err
	}

	return &CalicoClient{client: *client}, nil
}

func (c *CalicoClient) AssignIPv4(podId string) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	assignArgs := client.AutoAssignArgs{Num4: 1, Num6: 0, HandleID: &podId, Hostname: hostname}
	ipv4, _, err := c.client.IPAM().AutoAssign(assignArgs)
	num4 := len(ipv4)
	if num4 != 1 {
		return "", fmt.Errorf("Calico IPAM returned %d IPv4 addresses", num4)
	}

	return ipv4[0].String(), err
}

func (c *CalicoClient) ReleaseByPodId(podId string) error {
	return c.client.IPAM().ReleaseByHandle(podId)
}
