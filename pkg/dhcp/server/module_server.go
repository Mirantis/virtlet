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
	"net"
	"sync"

	"github.com/Mirantis/virtlet/pkg/dhcp"
)

// moduleServer contains common parts for different type internet servers
type moduleServer struct {
	listener      net.Listener
	mutex         *sync.Mutex
	configuration *dhcp.Configuration
}

// Close performs server cleanup
func (s moduleServer) Close() error {
	return s.listener.Close()
}

// SetupListener performs common listener setup
func (s moduleServer) SetupListener(family, laddr string) error {
	var err error
	if s.listener, err = net.Listen(family, laddr); err != nil {
		return err
	}

	return nil
}
