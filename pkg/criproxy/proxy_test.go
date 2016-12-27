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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

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

func pstr(s string) *string {
	return &s
}

func puint64(v uint64) *uint64 {
	return &v
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
	// imageClient := runtimeapi.NewImageServiceClient(conn)

	fakeImageNames := []string{"image1", "image2"}
	criServer.SetFakeImages(fakeImageNames)
	criServer.SetFakeImageSize(fakeImageSize)

	for _, step := range []struct {
		name, method string
		in, resp     interface{}
	}{
		{
			// TODO: test image filtering
			name:   "list images",
			method: "/runtime.ImageService/ListImages",
			in:     &runtimeapi.ListImagesRequest{},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       pstr("image1"),
						RepoTags: []string{"image1"},
						Size_:    puint64(fakeImageSize),
					},
					{
						Id:       pstr("image2"),
						RepoTags: []string{"image2"},
						Size_:    puint64(fakeImageSize),
					},
				},
			},
		},
	} {
		t.Run(step.name, func(t *testing.T) {
			actualResponse := reflect.New(reflect.TypeOf(step.resp).Elem()).Interface()
			if err := grpc.Invoke(context.Background(), step.method, step.in, actualResponse, conn); err != nil {
				t.Fatalf("GRPC call failed: %v", err)
			}

			if !reflect.DeepEqual(actualResponse, step.resp) {
				expectedJSON, err := json.MarshalIndent(step.resp, "", "  ")
				if err != nil {
					t.Fatalf("Failed to marshal json: %v", err)
				}
				actualJSON, err := json.MarshalIndent(actualResponse, "", "  ")
				if err != nil {
					t.Fatalf("Failed to marshal json: %v", err)
				}
				diff := difflib.UnifiedDiff{
					A:        difflib.SplitLines(string(expectedJSON)),
					B:        difflib.SplitLines(string(actualJSON)),
					FromFile: "expected",
					ToFile:   "actual",
					Context:  5,
				}
				diffText, _ := difflib.GetUnifiedDiffString(diff)
				fmt.Println(diffText)
				t.Errorf("Response diff:\n%s", diffText)
			}
		})
	}
}
