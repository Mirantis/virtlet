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

package criproxy

import (
	"testing"
	"time"

	// "github.com/davecgh/go-spew/spew"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"reflect"

	proxytest "github.com/Mirantis/virtlet/pkg/criproxy/testing"
)

const (
	fakeCriSocketPath         = "/tmp/fake-cri.socket"
	criProxySocketForTests    = "/tmp/cri-proxy.socket"
	connectionTimeoutForTests = 20 * time.Second
	fakeImageSize             = uint64(424242)
)

type ServerWithReadinessFeedback interface {
	Serve(addr string, readyCh chan struct{}) error
}

func startServer(t *testing.T, s ServerWithReadinessFeedback, addr string) {
	readyCh := make(chan struct{})
	errCh := make(chan error)
	go func() {
		if err := s.Serve(addr, readyCh); err != nil {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		t.Fatalf("Failed to start fake CRI server: %v", err)
	case <-readyCh:
	}
}

func TestCriProxy(t *testing.T) {
	criServer := proxytest.NewFakeCriServer()
	defer criServer.Stop()
	// TODO: don't wait for the server to start, the proxy should do it
	startServer(t, criServer, fakeCriSocketPath)

	proxy, err := NewRuntimeProxy(fakeCriSocketPath, connectionTimeoutForTests)
	if err != nil {
		// TODO: NewRuntimeProxy() shouldn't do any real work
		t.Fatalf("Failed to set up CRI proxy: %v", err)
	}
	defer proxy.Stop()
	startServer(t, proxy, criProxySocketForTests)

	conn, err := grpc.Dial(criProxySocketForTests, grpc.WithInsecure(), grpc.WithTimeout(connectionTimeoutForTests), grpc.WithDialer(dial))
	if err != nil {
		t.Fatalf("Connect remote runtime %s failed: %v", fakeCriSocketPath, err)
	}
	defer conn.Close()
	imageClient := runtimeapi.NewImageServiceClient(conn)

	fakeImageNames := []string{"image1", "image2"}
	criServer.SetFakeImages(fakeImageNames)
	criServer.SetFakeImageSize(fakeImageSize)

	resp, err := imageClient.ListImages(context.Background(), &runtimeapi.ListImagesRequest{})
	if err != nil {
		t.Fatalf("ListImages() failed: %v", err)
	}

	var repoTags []string
	for _, image := range resp.GetImages() {
		imageRepoTags := image.GetRepoTags()
		if len(imageRepoTags) != 1 {
			t.Errorf("bad repo tags for image: %#v", imageRepoTags)
			continue
		}
		if image.GetSize_() != fakeImageSize {
			t.Errorf("bad image size for image %q: %v instead of %v", imageRepoTags[0], image.GetSize_(), fakeImageSize)
		}
		repoTags = append(repoTags, imageRepoTags[0])
	}
	if !reflect.DeepEqual(fakeImageNames, repoTags) {
		t.Errorf("bad image tags returned: %#v instead of %#v", repoTags, fakeImageNames)
	}
}
