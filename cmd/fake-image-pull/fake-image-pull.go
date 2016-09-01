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
	"net"
	"os"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

var (
	imageUrl = flag.String("image-url",
		"http://ftp.ps.pl/pub/Linux/fedora-linux/releases/24/CloudImages/x86_64/images/Fedora-Cloud-Base-24-1.2.x86_64.qcow2",
		"Image URL to pull")
	virtletSocket = flag.String("virtlet-socket",
		"/run/virtlet.sock",
		"The unix socket to connect, e.g. /run/virtlet.sock")
)

func dial(socket string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socket, timeout)
}

func main() {
	flag.Parse()

	conn, err := grpc.Dial(*virtletSocket, grpc.WithInsecure(), grpc.WithDialer(dial))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect: %#v", err)
		os.Exit(1)
	}
	defer conn.Close()
	c := kubeapi.NewImageServiceClient(conn)

	imageSpec := &kubeapi.ImageSpec{Image: imageUrl}
	in := &kubeapi.PullImageRequest{
		Image: imageSpec,
		Auth: &kubeapi.AuthConfig{},
		SandboxConfig: &kubeapi.PodSandboxConfig{},
	}

	out, err := c.PullImage(context.Background(), in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot pull image: %#v", err)
		os.Exit(1)
	}

	fmt.Printf("Got response: %#v\n", out)
}
