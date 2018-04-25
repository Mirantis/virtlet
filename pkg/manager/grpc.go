/*
Copyright 2018 Mirantis

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

package manager

import (
	"net"
	"os"
	"syscall"

	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

type Server struct {
	server *grpc.Server
}

func NewServer() *Server {
	return &Server{
		server: grpc.NewServer(),
	}
}

// Serve set up a listener on unix socket, than it passes that listener to
// main loop of grpc server which handles CRI calls.
func (s *Server) Serve(addr string) error {
	if err := syscall.Unlink(addr); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	return s.server.Serve(ln)
}

// Stop halts the manager.
func (s *Server) Stop() {
	s.server.Stop()
}

// Register registers CRI Runtime and Image services
func (s *Server) Register(runtimeService kubeapi.RuntimeServiceServer, imageService kubeapi.ImageServiceServer) {
	kubeapi.RegisterRuntimeServiceServer(s.server, runtimeService)
	kubeapi.RegisterImageServiceServer(s.server, imageService)
}
