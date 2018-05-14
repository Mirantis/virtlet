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
	"bytes"
	"fmt"
	"io"
	"net"
	"os/exec"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/docker/docker/pkg/pools"
	"github.com/golang/glog"

	"k8s.io/client-go/tools/remotecommand"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
)

// GetAttach returns attach stream request
func (s *Server) GetAttach(req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	return s.streamServer.GetAttach(req)
}

// GetPortForward returns pofrforward stream request
func (s *Server) GetPortForward(req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	return s.streamServer.GetPortForward(req)
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
		glog.Infof("Got a resize event: %+v", size)
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

// PortForward endpoint for streaming.Runtime
func (s *Server) PortForward(podSandboxID string, port int32, stream io.ReadWriteCloser) error {
	// implementation based on https://github.com/kubernetes-incubator/cri-o/blob/master/server/container_portforward.go
	glog.V(1).Infoln("New PortForward request", podSandboxID)

	socatPath, lookupErr := exec.LookPath("socat")
	if lookupErr != nil {
		return fmt.Errorf("unable to do port forwarding: socat not found")
	}

	ip, err := s.getPodSandboxIP(podSandboxID)
	if err != nil {
		return fmt.Errorf("unable to do port forwarding: %v", err)
	}

	args := []string{"-", fmt.Sprintf("TCP4:%s:%d", ip, port)}

	command := exec.Command(socatPath, args...)
	command.Stdout = stream

	stderr := new(bytes.Buffer)
	command.Stderr = stderr

	// If we use Stdin, command.Run() won't return until the goroutine that's copying
	// from stream finishes. Unfortunately, if you have a client like telnet connected
	// via port forwarding, as long as the user's telnet client is connected to the user's
	// local listener that port forwarding sets up, the telnet session never exits. This
	// means that even if socat has finished running, command.Run() won't ever return
	// (because the client still has the connection and stream open).
	//
	// The work around is to use StdinPipe(), as Wait() (called by Run()) closes the pipe
	// when the command (socat) exits.
	inPipe, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("unable to do port forwarding: error creating stdin pipe: %v", err)
	}
	go func() {
		pools.Copy(inPipe, stream)
		inPipe.Close()
	}()

	if err := command.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, stderr.String())
	}

	return nil
}

func (s *Server) getPodSandboxIP(sandboxID string) (string, error) {
	sandbox := s.metadataStore.PodSandbox(sandboxID)
	sandboxInfo, err := sandbox.Retrieve()
	if err != nil {
		glog.Errorf("Error when getting pod sandbox %q: %v", sandboxID, err)
		return "", err
	}
	if sandboxInfo == nil {
		glog.Errorf("Missing metadata for pod sandbox %q", sandboxID)
		return "", fmt.Errorf("missing metadata for pod sandbox %q", sandboxID)
	}

	if sandboxInfo.ContainerSideNetwork == nil {
		return "", fmt.Errorf("ContainerSideNetwork missing in PodSandboxInfo returned from medatada store")
	}
	ip := cni.GetPodIP(sandboxInfo.ContainerSideNetwork.Result)
	if ip != "" {
		return ip, nil
	}
	return "", fmt.Errorf("Couldn't get IP address for for PodSandbox: %s", sandboxID)
}
