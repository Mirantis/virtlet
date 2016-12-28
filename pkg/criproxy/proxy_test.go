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
	fakeCriSocketPath1        = "/tmp/fake-cri-1.socket"
	fakeCriSocketPath2        = "/tmp/fake-cri-2.socket"
	altSocketSpec             = "alt:" + fakeCriSocketPath2
	criProxySocketForTests    = "/tmp/cri-proxy.socket"
	connectionTimeoutForTests = 20 * time.Second
	fakeImageSize1            = uint64(424242)
	fakeImageSize2            = uint64(434343)
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

func pbool(b bool) *bool {
	return &b
}

func puint64(v uint64) *uint64 {
	return &v
}

func TestCriProxy(t *testing.T) {
	journal := proxytest.NewSimpleJournal()
	criServer1 := proxytest.NewFakeCriServer(proxytest.NewPrefixJournal(journal, "1/"))
	defer criServer1.Stop()
	criServer2 := proxytest.NewFakeCriServer(proxytest.NewPrefixJournal(journal, "2/"))
	defer criServer2.Stop()
	// TODO: don't wait for the servers to start, the proxy should do it
	startServer(t, criServer1, fakeCriSocketPath1)
	startServer(t, criServer2, fakeCriSocketPath2)

	proxy := NewRuntimeProxy([]string{fakeCriSocketPath1, altSocketSpec}, connectionTimeoutForTests)
	if err := proxy.Connect(); err != nil {
		// TODO: NewRuntimeProxy() shouldn't do any real work
		t.Fatalf("Failed to set up CRI proxy: %v", err)
	}
	defer proxy.Stop()
	startServer(t, proxy, criProxySocketForTests)

	conn, err := grpc.Dial(criProxySocketForTests, grpc.WithInsecure(), grpc.WithTimeout(connectionTimeoutForTests), grpc.WithDialer(dial))
	if err != nil {
		t.Fatalf("Connect remote runtime %s failed: %v", criProxySocketForTests, err)
	}
	defer conn.Close()

	fakeImageNames1 := []string{"image1-1", "image1-2"}
	criServer1.SetFakeImages(fakeImageNames1)
	criServer1.SetFakeImageSize(fakeImageSize1)

	fakeImageNames2 := []string{"image2-1", "image2-2"}
	criServer2.SetFakeImages(fakeImageNames2)
	criServer2.SetFakeImageSize(fakeImageSize2)

	for _, step := range []struct {
		name, method string
		in, resp     interface{}
		journal      []string
	}{
		{
			name:   "version",
			method: "/runtime.RuntimeService/Version",
			in:     &runtimeapi.VersionRequest{},
			resp: &runtimeapi.VersionResponse{
				Version:           pstr("0.1.0"),
				RuntimeName:       pstr("fakeRuntime"),
				RuntimeVersion:    pstr("0.1.0"),
				RuntimeApiVersion: pstr("0.1.0"),
			},
			journal: []string{"1/runtime/Version"},
		},
		{
			name:   "status",
			method: "/runtime.RuntimeService/Status",
			in:     &runtimeapi.StatusRequest{},
			resp: &runtimeapi.StatusResponse{
				Status: &runtimeapi.RuntimeStatus{
					Conditions: []*runtimeapi.RuntimeCondition{
						{
							Type:   pstr("RuntimeReady"),
							Status: pbool(true),
						},
						{
							Type:   pstr("NetworkReady"),
							Status: pbool(true),
						},
					},
				},
			},
			// FIXME: actually, both runtimes need to be contacted and
			// the result needs to be combined
			journal: []string{"1/runtime/Status"},
		},
		{
			name:   "list images",
			method: "/runtime.ImageService/ListImages",
			in:     &runtimeapi.ListImagesRequest{},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       pstr("image1-1"),
						RepoTags: []string{"image1-1"},
						Size_:    puint64(fakeImageSize1),
					},
					{
						Id:       pstr("image1-2"),
						RepoTags: []string{"image1-2"},
						Size_:    puint64(fakeImageSize1),
					},
					{
						Id:       pstr("alt/image2-1"),
						RepoTags: []string{"alt/image2-1"},
						Size_:    puint64(fakeImageSize2),
					},
					{
						Id:       pstr("alt/image2-2"),
						RepoTags: []string{"alt/image2-2"},
						Size_:    puint64(fakeImageSize2),
					},
				},
			},
			journal: []string{"1/image/ListImages", "2/image/ListImages"},
		},
		{
			name:   "pull image (primary)",
			method: "/runtime.ImageService/PullImage",
			in: &runtimeapi.PullImageRequest{
				Image:         &runtimeapi.ImageSpec{Image: pstr("image1-3")},
				Auth:          &runtimeapi.AuthConfig{},
				SandboxConfig: &runtimeapi.PodSandboxConfig{},
			},
			resp:    &runtimeapi.PullImageResponse{},
			journal: []string{"1/image/PullImage"},
		},
		{
			name:   "pull image (alt)",
			method: "/runtime.ImageService/PullImage",
			in: &runtimeapi.PullImageRequest{
				Image:         &runtimeapi.ImageSpec{Image: pstr("alt/image2-3")},
				Auth:          &runtimeapi.AuthConfig{},
				SandboxConfig: &runtimeapi.PodSandboxConfig{},
			},
			resp:    &runtimeapi.PullImageResponse{},
			journal: []string{"2/image/PullImage"},
		},
		{
			name:   "list pulled image 1",
			method: "/runtime.ImageService/ListImages",
			in: &runtimeapi.ListImagesRequest{
				Filter: &runtimeapi.ImageFilter{
					Image: &runtimeapi.ImageSpec{Image: pstr("image1-3")},
				},
			},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       pstr("image1-3"),
						RepoTags: []string{"image1-3"},
						Size_:    puint64(fakeImageSize1),
					},
				},
			},
			journal: []string{"1/image/ListImages"},
		},
		{
			name:   "list pulled image 2",
			method: "/runtime.ImageService/ListImages",
			in: &runtimeapi.ListImagesRequest{
				Filter: &runtimeapi.ImageFilter{
					Image: &runtimeapi.ImageSpec{Image: pstr("alt/image2-3")},
				},
			},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       pstr("alt/image2-3"),
						RepoTags: []string{"alt/image2-3"},
						Size_:    puint64(fakeImageSize2),
					},
				},
			},
			journal: []string{"2/image/ListImages"},
		},
		{
			name:   "image status 1-2",
			method: "/runtime.ImageService/ImageStatus",
			in: &runtimeapi.ImageStatusRequest{
				Image: &runtimeapi.ImageSpec{Image: pstr("image1-2")},
			},
			resp: &runtimeapi.ImageStatusResponse{
				Image: &runtimeapi.Image{
					Id:       pstr("image1-2"),
					RepoTags: []string{"image1-2"},
					Size_:    puint64(fakeImageSize1),
				},
			},
			journal: []string{"1/image/ImageStatus"},
		},
		{
			name:   "image status 2-3",
			method: "/runtime.ImageService/ImageStatus",
			in: &runtimeapi.ImageStatusRequest{
				Image: &runtimeapi.ImageSpec{Image: pstr("alt/image2-3")},
			},
			resp: &runtimeapi.ImageStatusResponse{
				Image: &runtimeapi.Image{
					Id:       pstr("alt/image2-3"),
					RepoTags: []string{"alt/image2-3"},
					Size_:    puint64(fakeImageSize2),
				},
			},
			journal: []string{"2/image/ImageStatus"},
		},
		{
			name:   "remove image 1-1",
			method: "/runtime.ImageService/RemoveImage",
			in: &runtimeapi.RemoveImageRequest{
				Image: &runtimeapi.ImageSpec{Image: pstr("image1-1")},
			},
			resp:    &runtimeapi.RemoveImageResponse{},
			journal: []string{"1/image/RemoveImage"},
		},
		{
			name:   "remove image 2-2",
			method: "/runtime.ImageService/RemoveImage",
			in: &runtimeapi.RemoveImageRequest{
				Image: &runtimeapi.ImageSpec{Image: pstr("alt/image2-2")},
			},
			resp:    &runtimeapi.RemoveImageResponse{},
			journal: []string{"2/image/RemoveImage"},
		},
		{
			name:   "relist images after removing some of them",
			method: "/runtime.ImageService/ListImages",
			in:     &runtimeapi.ListImagesRequest{},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       pstr("image1-2"),
						RepoTags: []string{"image1-2"},
						Size_:    puint64(fakeImageSize1),
					},
					{
						Id:       pstr("image1-3"),
						RepoTags: []string{"image1-3"},
						Size_:    puint64(fakeImageSize1),
					},
					{
						Id:       pstr("alt/image2-1"),
						RepoTags: []string{"alt/image2-1"},
						Size_:    puint64(fakeImageSize2),
					},
					{
						Id:       pstr("alt/image2-3"),
						RepoTags: []string{"alt/image2-3"},
						Size_:    puint64(fakeImageSize2),
					},
				},
			},
			journal: []string{"1/image/ListImages", "2/image/ListImages"},
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
				t.Errorf("Response diff:\n%s", diffText)
			}

			if err := journal.Verify(step.journal); err != nil {
				t.Error(err)
			}
		})
	}
}

// TODO: proper status handling (contact both runtimes, etc.)
