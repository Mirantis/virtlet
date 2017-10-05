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
	"io"
	"net"

	"github.com/golang/glog"

	"k8s.io/client-go/tools/remotecommand"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
)

// GetAttach returns attach stream request
func (s *Server) GetAttach(req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	return s.streamServer.GetAttach(req)
}

// Attach endpoint for streaming.Runtime
func (s *Server) Attach(containerID string, inputStream io.Reader, outputStream, errorStream io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	glog.V(1).Infoln("New Attach request", containerID)
	c, ok := s.unixServer.UnixConnections.Load(containerID)
	if ok == false {
		return fmt.Errorf("could not find vm %q", containerID)
	}
	conn := c.(*net.UnixConn)

	outChan := make(chan []byte)
	s.unixServer.AddOutputReader(containerID, outChan)

	kubecontainer.HandleResizing(resize, func(size remotecommand.TerminalSize) {
		glog.Info("Got a resize event: %+v", size)
	})

	receiveStdout := make(chan error)
	if outputStream != nil {
		go func() {
			for data := range outChan {
				outputStream.Write(data)
			}
		}()
	}

	stdinDone := make(chan error)
	go func() {
		var err error
		if inputStream != nil {
			_, err = CopyDetachable(conn, inputStream, nil)
			if err != nil {
				glog.V(1).Info("Attach coppy error: %v", err)
			}
			stdinDone <- err
		}
	}()

	var err error
	select {
	case err = <-receiveStdout:
	case err = <-stdinDone:
	}
	s.unixServer.RemoveOutputReader(containerID, outChan)
	glog.V(1).Infoln("Attach request finished", containerID)
	return err
}
