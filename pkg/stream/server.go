/*
Copyright 2017 Mirantis

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

package stream

import (
	"fmt"
	"net"
	"os"
	"syscall"

	knet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
)

// Server implements the RuntimeService and ImageService
type Server struct {
	DeadlineSeconds int
	exitMonitorChan chan struct{}

	unixServer *UnixServer

	streamServer        streaming.Server
	streamServerCloseCh chan struct{}
	streaming.Runtime
}

// New creates a new Server with options provided
func NewServer(kubernetesDir, socketPath string) (*Server, error) {
	s := &Server{DeadlineSeconds: 10}

	// Prepare unix server
	s.unixServer = NewUnixServer(socketPath, kubernetesDir)

	bindAddress, err := knet.ChooseBindAddress(net.IP{0, 0, 0, 0})
	if err != nil {
		return nil, err
	}
	streamPort := "10010"

	streamServerConfig := streaming.DefaultConfig
	streamServerConfig.Addr = net.JoinHostPort(bindAddress.String(), streamPort)
	s.streamServer, err = streaming.NewServer(streamServerConfig, s)
	if err != nil {
		return nil, fmt.Errorf("unable to create streaming server")
	}

	return s, nil
}

func (s *Server) Start() error {
	if err := syscall.Unlink(s.unixServer.SocketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	// start http server
	go func() {
		if err := s.streamServer.Start(true); err != nil {
			fmt.Errorf("Failed to start streaming server: %v", err)
		}
	}()
	// start socket server
	go s.unixServer.Listen()
	return nil

}

func (s *Server) Stop() {
	// in k8s 1.7 Stop() does nothing, starting from 1.8 it will stop streaming server
	s.streamServer.Stop()
	s.unixServer.Stop()
}
