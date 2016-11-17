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
	"crypto/rand"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/Mirantis/virtlet/pkg/dhcp"
)

type DHCPClient struct {
	client dhcp.DHCPConfigurationClient
}

func dial(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}

// NewDHCPClient returns new client which can manipulate configurations
// on our dhcp server
func NewDHCPClient() (*DHCPClient, error) {
	conn, err := grpc.Dial("/run/virtlet-dhcp.sock", grpc.WithInsecure(), grpc.WithDialer(dial))
	if err != nil {
		return nil, err
	}

	return &DHCPClient{
		client: dhcp.NewDHCPConfigurationClient(conn),
	}, nil
}

// CreateNewEndpoint prepares dhcp configuration for desired ip address
// configures dhcp server for it and returns random mac address for which
// this configuration was set on dhcp server
func (c *DHCPClient) CreateNewEndpoint(ipv4 string, routes map[string]string) ([]byte, error) {
	mac, err := generateMacAddress()
	if err != nil {
		return []byte{}, err
	}
	transformedRoutes := prepareRoutes(routes)
	endpConf := dhcp.EndpointConfiguration{
		Endpoint: &dhcp.Endpoint{
			HardwareAddress: []byte(mac),
		},
		Ipv4Address: &ipv4,
		Routes:      transformedRoutes,
	}
	_, err = c.client.SetConfiguration(context.Background(), &endpConf)
	if err != nil {
		return []byte{}, err
	}

	return []byte(mac), nil
}

// copied from:
// https://github.com/coreos/rkt/blob/56564bac090b44788684040f2ffd66463f29d5d0/stage1/init/kvm/network.go#L71
func generateMacAddress() ([]byte, error) {
	mac := []byte{
		2,          // locally administred unicast
		0x65, 0x02, // OUI (randomly chosen by jell)
		0, 0, 0, // bytes to randomly overwrite
	}

	_, err := rand.Read(mac[3:6])
	if err != nil {
		return nil, fmt.Errorf("cannot generate random mac address: %v", err)
	}

	return mac, nil
}

func prepareRoutes(stringified_routes map[string]string) []*dhcp.EndpointConfiguration_Route {
	var routes []*dhcp.EndpointConfiguration_Route
	for destination, router := range stringified_routes {
		routes = append(routes, &dhcp.EndpointConfiguration_Route{
			Destination: &destination,
			Through:     &router,
		})
	}
	return routes
}

func (c *DHCPClient) RemoveEndpoint(hwAddress []byte) error {
	_, err := c.client.RemoveConfiguration(context.Background(), &dhcp.Endpoint{
		HardwareAddress: hwAddress,
	})
	return err
}
