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

package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/Mirantis/virtlet/pkg/utils"
)

var (
	imageUrl = flag.String("image-url",
		"http://ftp.ps.pl/pub/Linux/fedora-linux/releases/24/CloudImages/x86_64/images/Fedora-Cloud-Base-24-1.2.x86_64.qcow2",
		"Image URL to pull")
	virtletSocket = flag.String("virtlet-socket",
		"/run/virtlet.sock",
		"The unix socket to connect, e.g. /run/virtlet.sock")
)

func main() {
	flag.Parse()

	conn, err := grpc.Dial(*virtletSocket, grpc.WithInsecure(), grpc.WithDialer(utils.Dial))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect: %#v", err)
		os.Exit(1)
	}
	defer conn.Close()
	c := kubeapi.NewRuntimeServiceClient(conn)

	// Sandbox request
	hostNetwork := false
	hostPid := false
	hostIpc := false
	namespaceOptions := &kubeapi.NamespaceOption{
		HostNetwork: &hostNetwork,
		HostPid:     &hostPid,
		HostIpc:     &hostIpc,
	}

	cgroupParent := ""
	linuxPodSandboxConfig := &kubeapi.LinuxPodSandboxConfig{
		CgroupParent:     &cgroupParent,
		NamespaceOptions: namespaceOptions,
	}

	podSandboxName := "foo"
	// podSandboxUid := ""
	podSandboxNamespace := "default"
	podSandboxMetadata := &kubeapi.PodSandboxMetadata{
		Name:      &podSandboxName,
		Namespace: &podSandboxNamespace,
	}

	podSandboxHostname := "localhost"

	podSandboxConfig := &kubeapi.PodSandboxConfig{
		Metadata: podSandboxMetadata,
		Hostname: &podSandboxHostname,
		Linux:    linuxPodSandboxConfig,
	}
	sandboxIn := &kubeapi.RunPodSandboxRequest{Config: podSandboxConfig}

	sandboxOut, err := c.RunPodSandbox(context.Background(), sandboxIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create pod sandbox: %#v", err)
		os.Exit(1)
	}

	// Container request
	imageSpec := &kubeapi.ImageSpec{Image: imageUrl}
	config := &kubeapi.ContainerConfig{
		Image: imageSpec,
	}
	containerIn := &kubeapi.CreateContainerRequest{
		PodSandboxId:  sandboxOut.PodSandboxId,
		Config:        config,
		SandboxConfig: podSandboxConfig,
	}

	containerOut, err := c.CreateContainer(context.Background(), containerIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create container: %#v", err)
		os.Exit(1)
	}

	fmt.Printf("Got response: %#v\n", containerOut)
	fmt.Printf("Created container with ID: %s\n", *containerOut.ContainerId)
}
