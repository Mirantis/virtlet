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

package cni

import (
	"fmt"

	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/types"
)

type Client struct {
	pluginsInterface libcni.CNIConfig
	configuration    *libcni.NetworkConfig
}

func NewClient(pluginsDir, configsDir string) (*Client, error) {
	configuration, err := ReadConfiguration(configsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read CNI configuration: %v", err)
	}

	return &Client{
		pluginsInterface: libcni.CNIConfig{Path: []string{pluginsDir}},
		configuration:    configuration,
	}, nil
}

func (c *Client) AddSandboxToNetwork(podId string) (*types.Result, error) {
	netnsPath := PodNetNSPath(podId)
	rt := &libcni.RuntimeConf{
		ContainerID: podId,
		NetNS:       netnsPath,
		IfName:      "virtlet-eth0",
	}
	return c.pluginsInterface.AddNetwork(c.configuration, rt)
}

func (c *Client) RemoveSandboxFromNetwork(podId string) error {
	netnsPath := PodNetNSPath(podId)
	rt := &libcni.RuntimeConf{
		ContainerID: podId,
		NetNS:       netnsPath,
		IfName:      "virtlet-eth0",
	}
	return c.pluginsInterface.DelNetwork(c.configuration, rt)
}
