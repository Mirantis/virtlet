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

package server

import (
	"bytes"
	"fmt"
	"net"
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/Mirantis/virtlet/pkg/dhcp"
)

type configManipulatorServer struct {
	mutex   *sync.Mutex
	configs *dhcp.Configuration
}

func (s *configManipulatorServer) SetConfiguration(ctx context.Context, config *dhcp.EndpointConfiguration) (*dhcp.SetConfigurationResponse, error) {
	found := false

	s.mutex.Lock()
	defer s.mutex.Unlock()

	for i, configuration := range s.configs.EndpointConfigurations {
		if bytes.Equal(config.GetEndpoint().GetHardwareAddress(), configuration.GetEndpoint().GetHardwareAddress()) {
			s.configs.EndpointConfigurations[i] = config
			found = true
			break
		}
	}
	if found != true {
		s.configs.EndpointConfigurations = append(s.configs.EndpointConfigurations, config)
	}

	s.configs.Save()
	return &dhcp.SetConfigurationResponse{}, nil
}

func findEndpointConfigurationIndex(configs []*dhcp.EndpointConfiguration, hwaddr []byte) (int, error) {
	for i, config := range configs {
		if bytes.Equal(config.GetEndpoint().GetHardwareAddress(), hwaddr) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("configuration not found for requested mac address: %v", hwaddr)
}

func (s *configManipulatorServer) RemoveConfiguration(ctx context.Context, endpoint *dhcp.Endpoint) (*dhcp.RemoveConfigurationResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	cf := s.configs.EndpointConfigurations
	i, err := findEndpointConfigurationIndex(cf, endpoint.GetHardwareAddress())
	if err != nil {
		return nil, err
	}

	s.configs.EndpointConfigurations = append(cf[:i], cf[i+1:]...)
	s.configs.Save()
	return &dhcp.RemoveConfigurationResponse{}, nil
}

type configManipulator struct {
	listener      net.Listener
	mutex         *sync.Mutex
	configuration *dhcp.Configuration
	grpcServer    *grpc.Server
}

func (s *configManipulator) SetupListener(family, laddr string) error {
	var err error
	if s.listener, err = net.Listen(family, laddr); err != nil {
		return err
	}

	return nil
}

func (s *configManipulator) Serve() error {
	s.grpcServer = grpc.NewServer()
	dhcp.RegisterDHCPConfigurationServer(s.grpcServer, &configManipulatorServer{
		mutex:   s.mutex,
		configs: s.configuration,
	})
	return s.grpcServer.Serve(s.listener)
}

func (s *configManipulator) Close() error {
	s.grpcServer.Stop()
	return s.listener.Close()
}
