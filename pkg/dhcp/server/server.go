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
	"fmt"
	"sync"

	"github.com/Mirantis/virtlet/pkg/dhcp"
)

type server struct {
	configuration *dhcp.Configuration
	mutex         sync.Mutex
}

// NewServer returns server instance with loaded endpoints configuration
// stored in previous run of this server
func NewServer(configDBPath string) (*server, error) {
	configuration, err := dhcp.NewConfiguration(configDBPath)
	if err != nil {
		return nil, err
	}
	return &server{configuration: configuration}, nil
}

// Serve prepares and runs configuration manipulator and dhcp server
func (s server) Serve(configSocketPath, dhcpIp string) error {
	// 2 buffer slots, one per each goroutine
	// when we will want to add graceful shutdown, we should add there slot
	// for returning nil from it
	errors := make(chan error, 2)

	configManipulator, err := s.NewConfigManipulator(configSocketPath)
	if err != nil {
		return fmt.Errorf("Can not listen on socket %s: %v", configSocketPath, err)
	}
	defer configManipulator.Close()

	dhcp, err := s.NewDHCPServer(dhcpIp)
	if err != nil {
		return fmt.Errorf("Can not listen on dhcp udp port: %v", err)
	}
	defer dhcp.Close()

	go func() { errors <- configManipulator.Serve() }()
	go func() { errors <- dhcp.Serve() }()

	// Wait for error from any serving module
	err = <-errors

	return err
}

type ModuleServing interface {
	SetupListener(family, listenAddr string) error
	Serve() error
	Close() error
}

func (s server) NewConfigManipulator(socketPath string) (ModuleServing, error) {
	cm := configManipulator{moduleServer: moduleServer{mutex: &s.mutex, configuration: s.configuration}}
	if err := cm.SetupListener("unix", socketPath); err != nil {
		return nil, err
	}
	return cm, nil
}

func (s server) NewDHCPServer(IPToListen string) (ModuleServing, error) {
	dhcp := dHCPServer{moduleServer: moduleServer{mutex: &s.mutex, configuration: s.configuration}}
	if err := dhcp.SetupListener("not used", IPToListen+":67"); err != nil {
		return nil, err
	}
	return dhcp, nil
}
