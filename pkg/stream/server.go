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
	"strconv"
	"syscall"

	"github.com/Mirantis/virtlet/pkg/metadata"

	"github.com/golang/glog"
	knet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
)

// Server implements streaming.Runtime
type Server struct {
	DeadlineSeconds int
	exitMonitorChan chan struct{}

	unixServer *UnixServer

	streamServer        streaming.Server
	streamServerCloseCh chan struct{}
	streaming.Runtime

	metadataStore metadata.Store //required for port-forward
}

var _ streaming.Runtime = (*Server)(nil)

// NewServer creates a new Server
func NewServer(socketPath string, metadataStore metadata.Store, iStreamPort int) (*Server, error) {
	s := &Server{DeadlineSeconds: 10}

	// Prepare unix server
	s.unixServer = NewUnixServer(socketPath)

	bindAddress, err := knet.ChooseBindAddress(net.IP{0, 0, 0, 0})
	if err != nil {
		return nil, err
	}
	streamPort := strconv.Itoa(iStreamPort)

	streamServerConfig := streaming.DefaultConfig
	streamServerConfig.Addr = net.JoinHostPort(bindAddress.String(), streamPort)
	s.streamServer, err = streaming.NewServer(streamServerConfig, s)
	if err != nil {
		return nil, fmt.Errorf("unable to create streaming server")
	}

	s.metadataStore = metadataStore

	return s, nil
}

// Start starts streaming server gorutine and unixServer gorutine
func (s *Server) Start() error {
	if err := syscall.Unlink(s.unixServer.SocketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	// start http server
	go func() {
		if err := s.streamServer.Start(true); err != nil {
			glog.Fatalf("Failed to start streaming server: %v", err)
		}
	}()
	// start socket server
	go s.unixServer.Listen()
	return nil
}

// Stop stops all goroutines
func (s *Server) Stop() {
	// in k8s 1.7 Stop() does nothing, starting from 1.8 it will stop streaming server
	s.streamServer.Stop()
	s.unixServer.Stop()
}
