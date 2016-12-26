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

package testing

import (
	"net"
	"os"
	"syscall"

	"google.golang.org/grpc"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type FakeCriServer struct {
	*FakeRuntimeServer
	*FakeImageServer
	server *grpc.Server
}

func NewFakeCriServer() *FakeCriServer {
	s := &FakeCriServer{
		FakeRuntimeServer: NewFakeRuntimeServer(),
		FakeImageServer:   NewFakeImageServer(),
		server:            grpc.NewServer(),
	}
	runtimeapi.RegisterRuntimeServiceServer(s.server, s)
	runtimeapi.RegisterImageServiceServer(s.server, s)
	return s
}

func (s *FakeCriServer) Serve(addr string, readyCh chan struct{}) error {
	if err := syscall.Unlink(addr); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	if readyCh != nil {
		close(readyCh)
	}
	return s.server.Serve(ln)
}

func (s *FakeCriServer) Stop() {
	s.server.Stop()
}
