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

package criproxy

import (
	"encoding/json"
	"flag"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
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
	podUid1                   = "4bde9008-4663-4342-84ed-310cea787f95"
	podSandboxId1             = "pod-1-1_default_" + podUid1 + "_0"
	podUid2                   = "927a91df-f4d3-49a9-a257-5ca7f16f85fc"
	podSandboxId2             = "alt__pod-2-1_default_" + podUid2 + "_0"
	containerId1              = podSandboxId1 + "_container1_0"
	containerId2              = podSandboxId2 + "_container2_0"
)

type ServerWithReadinessFeedback interface {
	Serve(addr string, readyCh chan struct{}) error
}

func startServer(t *testing.T, s ServerWithReadinessFeedback, addr string) {
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		if err := s.Serve(addr, readyCh); err != nil {
			glog.Errorf("error starting server @ %q: %v", addr, err)
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		t.Fatalf("Failed to start fake CRI server: %v", err)
	case <-readyCh:
	}
	// TODO: don't use readiness feedback channel, it doesn't help much
	if err := waitForSocket(addr); err != nil {
		t.Fatalf("Waiting for socket %q failed: %v", addr, err)
	}
}

func pstr(s string) *string {
	return &s
}

func pbool(b bool) *bool {
	return &b
}

func pint32(v int32) *int32 {
	return &v
}

func puint32(v uint32) *uint32 {
	return &v
}

func puint64(v uint64) *uint64 {
	return &v
}

type proxyTester struct {
	hookCallCount int
	journal       *proxytest.SimpleJournal
	servers       []*proxytest.FakeCriServer
	proxy         *RuntimeProxy
	conn          *grpc.ClientConn
}

func newProxyTester(t *testing.T) *proxyTester {
	journal := proxytest.NewSimpleJournal()
	servers := []*proxytest.FakeCriServer{
		proxytest.NewFakeCriServer(proxytest.NewPrefixJournal(journal, "1/")),
		proxytest.NewFakeCriServer(proxytest.NewPrefixJournal(journal, "2/")),
	}

	fakeImageNames1 := []string{"image1-1", "image1-2"}
	servers[0].SetFakeImageSize(fakeImageSize1)
	servers[0].SetFakeImages(fakeImageNames1)

	fakeImageNames2 := []string{"image2-1", "image2-2"}
	servers[1].SetFakeImageSize(fakeImageSize2)
	servers[1].SetFakeImages(fakeImageNames2)

	tester := &proxyTester{
		journal: journal,
		servers: servers,
	}
	var err error
	tester.proxy, err = NewRuntimeProxy([]string{fakeCriSocketPath1, altSocketSpec}, connectionTimeoutForTests, func() {
		tester.hookCallCount++
	})
	if err != nil {
		t.Fatalf("failed to create runtime proxy: %v", err)
	}

	return tester
}

func (tester *proxyTester) startServers(t *testing.T, which int) {
	paths := []string{fakeCriSocketPath1, fakeCriSocketPath2}
	for i := 0; i < 2; i++ {
		if which < 0 || i == which {
			startServer(t, tester.servers[i], paths[i])
		}
	}
}

func (tester *proxyTester) startProxy(t *testing.T) {
	// if err := tester.proxy.Connect(); err != nil {
	// 	t.Fatalf("Failed to set up CRI proxy: %v", err)
	// }
	startServer(t, tester.proxy, criProxySocketForTests)
}

func (tester *proxyTester) connectToProxy(t *testing.T) {
	conn, err := grpc.Dial(criProxySocketForTests, grpc.WithInsecure(), grpc.WithTimeout(connectionTimeoutForTests), grpc.WithDialer(dial))
	if err != nil {
		t.Fatalf("Connect remote runtime %s failed: %v", criProxySocketForTests, err)
	}
	tester.conn = conn
}

func (tester *proxyTester) stop() {
	if tester.conn != nil {
		tester.conn.Close()
	}
	for _, server := range tester.servers {
		server.Stop()
	}
	tester.proxy.Stop()
}

func (tester *proxyTester) verifyJournal(t *testing.T, expectedJournal []string) {
	if err := tester.journal.Verify(expectedJournal); err != nil {
		t.Error(err)
	}
}

func (tester *proxyTester) invoke(method string, in, resp interface{}) error {
	return grpc.Invoke(context.Background(), method, in, resp, tester.conn)
}

func (tester *proxyTester) verifyCall(t *testing.T, method string, in, resp interface{}, expectedError string) {
	actualResponse := reflect.New(reflect.TypeOf(resp).Elem()).Interface()

	err := tester.invoke(method, in, actualResponse)
	switch {
	case expectedError == "" && err != nil:
		t.Errorf("GRPC call failed: %v", err)
	case expectedError != "" && err == nil:
		t.Errorf("did not get expected error")
	case expectedError != "" && !strings.Contains(err.Error(), expectedError):
		t.Errorf("bad error message: %q instead of %q", err.Error(), expectedError)
	}

	if err == nil && !reflect.DeepEqual(actualResponse, resp) {
		expectedJSON, err := json.MarshalIndent(resp, "", "  ")
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
}

func TestCriProxy(t *testing.T) {
	tester := newProxyTester(t)
	defer tester.stop()
	tester.startServers(t, -1)
	tester.startProxy(t)
	tester.connectToProxy(t)

	testCases := []struct {
		name, method string
		in, resp     interface{}
		ins          []interface{}
		journal      []string
		error        string
	}{
		{
			name:   "version",
			method: "/runtime.RuntimeService/Version",
			in:     &runtimeapi.VersionRequest{},
			resp: &runtimeapi.VersionResponse{
				Version:           "0.1.0",
				RuntimeName:       "fakeRuntime",
				RuntimeVersion:    "0.1.0",
				RuntimeApiVersion: "0.1.0",
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
							Type:   "RuntimeReady",
							Status: true,
						},
						{
							Type:   "NetworkReady",
							Status: true,
						},
					},
				},
			},
			// FIXME: actually, both runtimes need to be contacted and
			// the result needs to be combined
			journal: []string{"1/runtime/Status"},
		},
		{
			name:   "run pod sandbox 1",
			method: "/runtime.RuntimeService/RunPodSandbox",
			in: &runtimeapi.RunPodSandboxRequest{
				Config: &runtimeapi.PodSandboxConfig{
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      "pod-1-1",
						Uid:       podUid1,
						Namespace: "default",
						Attempt:   0,
					},
					Labels: map[string]string{"name": "pod-1-1"},
				},
			},
			resp: &runtimeapi.RunPodSandboxResponse{
				PodSandboxId: podSandboxId1,
			},
			journal: []string{"1/runtime/RunPodSandbox"},
		},
		{
			name:   "run pod sandbox 2",
			method: "/runtime.RuntimeService/RunPodSandbox",
			in: &runtimeapi.RunPodSandboxRequest{
				Config: &runtimeapi.PodSandboxConfig{
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      "pod-2-1",
						Uid:       podUid2,
						Namespace: "default",
						Attempt:   0,
					},
					Labels: map[string]string{"name": "pod-2-1"},
					Annotations: map[string]string{
						"kubernetes.io/target-runtime": "alt",
					},
				},
			},
			resp: &runtimeapi.RunPodSandboxResponse{
				PodSandboxId: podSandboxId2,
			},
			journal: []string{"2/runtime/RunPodSandbox"},
		},
		{
			name:   "run pod sandbox with bad runtime id",
			method: "/runtime.RuntimeService/RunPodSandbox",
			in: &runtimeapi.RunPodSandboxRequest{
				Config: &runtimeapi.PodSandboxConfig{
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      "pod-x-1",
						Uid:       podUid2,
						Namespace: "default",
						Attempt:   0,
					},
					Labels: map[string]string{"name": "pod-x-1"},
					Annotations: map[string]string{
						"kubernetes.io/target-runtime": "badruntime",
					},
				},
			},
			// resp must be specified even in case of expected error
			// because the type is needed to make the GRPC call
			resp:  &runtimeapi.RunPodSandboxResponse{},
			error: "criproxy: unknown runtime: \"badruntime\"",
		},
		{
			name:   "list pod sandboxes",
			method: "/runtime.RuntimeService/ListPodSandbox",
			in:     &runtimeapi.ListPodSandboxRequest{},
			resp: &runtimeapi.ListPodSandboxResponse{
				Items: []*runtimeapi.PodSandbox{
					{
						Id: podSandboxId1,
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      "pod-1-1",
							Uid:       podUid1,
							Namespace: "default",
							Attempt:   0,
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY,
						CreatedAt: tester.servers[0].CurrentTime,
						Labels:    map[string]string{"name": "pod-1-1"},
					},
					{
						Id: podSandboxId2,
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      "pod-2-1",
							Uid:       podUid2,
							Namespace: "default",
							Attempt:   0,
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY,
						CreatedAt: tester.servers[1].CurrentTime,
						Labels:    map[string]string{"name": "pod-2-1"},
						Annotations: map[string]string{
							"kubernetes.io/target-runtime": "alt",
						},
					},
				},
			},
			journal: []string{"1/runtime/ListPodSandbox", "2/runtime/ListPodSandbox"},
		},
		{
			name:   "list pod sandboxes with filter 1",
			method: "/runtime.RuntimeService/ListPodSandbox",
			in: &runtimeapi.ListPodSandboxRequest{
				Filter: &runtimeapi.PodSandboxFilter{Id: podSandboxId1},
			},
			resp: &runtimeapi.ListPodSandboxResponse{
				Items: []*runtimeapi.PodSandbox{
					{
						Id: podSandboxId1,
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      "pod-1-1",
							Uid:       podUid1,
							Namespace: "default",
							Attempt:   0,
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY,
						CreatedAt: tester.servers[0].CurrentTime,
						Labels:    map[string]string{"name": "pod-1-1"},
					},
				},
			},
			journal: []string{"1/runtime/ListPodSandbox"},
		},
		{
			name:   "list pod sandboxes with filter 2",
			method: "/runtime.RuntimeService/ListPodSandbox",
			in: &runtimeapi.ListPodSandboxRequest{
				Filter: &runtimeapi.PodSandboxFilter{Id: podSandboxId2},
			},
			resp: &runtimeapi.ListPodSandboxResponse{
				Items: []*runtimeapi.PodSandbox{
					{
						Id: podSandboxId2,
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      "pod-2-1",
							Uid:       podUid2,
							Namespace: "default",
							Attempt:   0,
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY,
						CreatedAt: tester.servers[1].CurrentTime,
						Labels:    map[string]string{"name": "pod-2-1"},
						Annotations: map[string]string{
							"kubernetes.io/target-runtime": "alt",
						},
					},
				},
			},
			journal: []string{"2/runtime/ListPodSandbox"},
		},
		{
			name:   "pod sandbox status 1",
			method: "/runtime.RuntimeService/PodSandboxStatus",
			in: &runtimeapi.PodSandboxStatusRequest{
				PodSandboxId: podSandboxId1,
			},
			resp: &runtimeapi.PodSandboxStatusResponse{
				Status: &runtimeapi.PodSandboxStatus{
					Id: podSandboxId1,
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      "pod-1-1",
						Uid:       podUid1,
						Namespace: "default",
						Attempt:   0,
					},
					State:     runtimeapi.PodSandboxState_SANDBOX_READY,
					CreatedAt: tester.servers[0].CurrentTime,
					Network: &runtimeapi.PodSandboxNetworkStatus{
						Ip: "192.168.192.168",
					},
					Labels: map[string]string{"name": "pod-1-1"},
				},
			},
			journal: []string{"1/runtime/PodSandboxStatus"},
		},
		{
			name:   "pod sandbox status 2",
			method: "/runtime.RuntimeService/PodSandboxStatus",
			in: &runtimeapi.PodSandboxStatusRequest{
				PodSandboxId: podSandboxId2,
			},
			resp: &runtimeapi.PodSandboxStatusResponse{
				Status: &runtimeapi.PodSandboxStatus{
					Id: podSandboxId2,
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      "pod-2-1",
						Uid:       podUid2,
						Namespace: "default",
						Attempt:   0,
					},
					State:     runtimeapi.PodSandboxState_SANDBOX_READY,
					CreatedAt: tester.servers[1].CurrentTime,
					Network: &runtimeapi.PodSandboxNetworkStatus{
						Ip: "192.168.192.168",
					},
					Labels: map[string]string{"name": "pod-2-1"},
					Annotations: map[string]string{
						"kubernetes.io/target-runtime": "alt",
					},
				},
			},
			journal: []string{"2/runtime/PodSandboxStatus"},
		},
		{
			name:   "create container 1",
			method: "/runtime.RuntimeService/CreateContainer",
			in: &runtimeapi.CreateContainerRequest{
				PodSandboxId: podSandboxId1,
				Config: &runtimeapi.ContainerConfig{
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    "container1",
						Attempt: 0,
					},
					Image: &runtimeapi.ImageSpec{
						Image: "image1-1",
					},
				},
			},
			resp: &runtimeapi.CreateContainerResponse{
				ContainerId: containerId1,
			},
			journal: []string{"1/runtime/CreateContainer"},
		},
		{
			name:   "create container 2",
			method: "/runtime.RuntimeService/CreateContainer",
			in: &runtimeapi.CreateContainerRequest{
				PodSandboxId: podSandboxId2,
				Config: &runtimeapi.ContainerConfig{
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    "container2",
						Attempt: 0,
					},
					Image: &runtimeapi.ImageSpec{
						Image: "alt/image2-1",
					},
				},
			},
			resp: &runtimeapi.CreateContainerResponse{
				ContainerId: containerId2,
			},
			journal: []string{"2/runtime/CreateContainer"},
		},
		{
			name:   "try to create container for a wrong runtime",
			method: "/runtime.RuntimeService/CreateContainer",
			in: &runtimeapi.CreateContainerRequest{
				PodSandboxId: podSandboxId2,
				Config: &runtimeapi.ContainerConfig{
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    "container2",
						Attempt: 0,
					},
					Image: &runtimeapi.ImageSpec{
						Image: "image1-2",
					},
				},
			},
			// resp must be specified even in case of expected error
			// because the type is needed to make the GRPC call
			resp:  &runtimeapi.CreateContainerResponse{},
			error: "criproxy: image \"image1-2\" is for a wrong runtime",
		},
		{
			name:   "list containers",
			method: "/runtime.RuntimeService/ListContainers",
			in:     &runtimeapi.ListContainersRequest{},
			resp: &runtimeapi.ListContainersResponse{
				Containers: []*runtimeapi.Container{
					{
						Id:           containerId1,
						PodSandboxId: podSandboxId1,
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    "container1",
							Attempt: 0,
						},
						Image: &runtimeapi.ImageSpec{
							Image: "image1-1",
						},
						ImageRef:  "image1-1",
						CreatedAt: tester.servers[0].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED,
					},
					{
						Id:           containerId2,
						PodSandboxId: podSandboxId2,
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    "container2",
							Attempt: 0,
						},
						Image: &runtimeapi.ImageSpec{
							Image: "alt/image2-1",
						},
						ImageRef:  "image2-1",
						CreatedAt: tester.servers[1].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED,
					},
				},
			},
			journal: []string{"1/runtime/ListContainers", "2/runtime/ListContainers"},
		},
		{
			name:   "list containers with container filter 1",
			method: "/runtime.RuntimeService/ListContainers",
			ins: []interface{}{
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{Id: containerId1},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{PodSandboxId: podSandboxId1},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           containerId1,
						PodSandboxId: podSandboxId1,
					},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           containerId1,
						PodSandboxId: podSandboxId1,
						State:        &runtimeapi.ContainerStateValue{State: runtimeapi.ContainerState_CONTAINER_CREATED},
					},
				},
			},
			resp: &runtimeapi.ListContainersResponse{
				Containers: []*runtimeapi.Container{
					{
						Id:           containerId1,
						PodSandboxId: podSandboxId1,
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    "container1",
							Attempt: 0,
						},
						Image: &runtimeapi.ImageSpec{
							Image: "image1-1",
						},
						ImageRef:  "image1-1",
						CreatedAt: tester.servers[0].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED,
					},
				},
			},
			journal: []string{"1/runtime/ListContainers"},
		},
		{
			name:   "list containers with container filter 2",
			method: "/runtime.RuntimeService/ListContainers",
			ins: []interface{}{
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{Id: containerId2},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{PodSandboxId: podSandboxId2},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           containerId2,
						PodSandboxId: podSandboxId2,
					},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           containerId2,
						PodSandboxId: podSandboxId2,
						State:        &runtimeapi.ContainerStateValue{State: runtimeapi.ContainerState_CONTAINER_CREATED},
					},
				},
			},
			resp: &runtimeapi.ListContainersResponse{
				Containers: []*runtimeapi.Container{
					{
						Id:           containerId2,
						PodSandboxId: podSandboxId2,
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    "container2",
							Attempt: 0,
						},
						Image: &runtimeapi.ImageSpec{
							Image: "alt/image2-1",
						},
						ImageRef:  "image2-1",
						CreatedAt: tester.servers[1].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED,
					},
				},
			},
			journal: []string{"2/runtime/ListContainers"},
		},
		{
			name:   "list containers with contradicting id+sandbox filters",
			method: "/runtime.RuntimeService/ListContainers",
			ins: []interface{}{
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           containerId1,
						PodSandboxId: podSandboxId2,
					},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           containerId1,
						PodSandboxId: podSandboxId2,
						State:        &runtimeapi.ContainerStateValue{State: runtimeapi.ContainerState_CONTAINER_CREATED},
					},
				},
			},
			resp: &runtimeapi.ListContainersResponse{},
			// note that runtimes' ListContainers() aren't even invoked in this case
		},
		{
			name:   "container status 1",
			method: "/runtime.RuntimeService/ContainerStatus",
			in: &runtimeapi.ContainerStatusRequest{
				ContainerId: containerId1,
			},
			resp: &runtimeapi.ContainerStatusResponse{
				Status: &runtimeapi.ContainerStatus{
					Id: containerId1,
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    "container1",
						Attempt: 0,
					},
					Image: &runtimeapi.ImageSpec{
						Image: "image1-1",
					},
					ImageRef:  "image1-1",
					CreatedAt: tester.servers[0].CurrentTime,
					State:     runtimeapi.ContainerState_CONTAINER_CREATED,
				},
			},
			journal: []string{"1/runtime/ContainerStatus"},
		},
		{
			name:   "container status 2",
			method: "/runtime.RuntimeService/ContainerStatus",
			in: &runtimeapi.ContainerStatusRequest{
				ContainerId: containerId2,
			},
			resp: &runtimeapi.ContainerStatusResponse{
				Status: &runtimeapi.ContainerStatus{
					Id: containerId2,
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    "container2",
						Attempt: 0,
					},
					Image: &runtimeapi.ImageSpec{
						Image: "alt/image2-1",
					},
					// ImageRef is not prefixed
					ImageRef:  "image2-1",
					CreatedAt: tester.servers[1].CurrentTime,
					State:     runtimeapi.ContainerState_CONTAINER_CREATED,
				},
			},
			journal: []string{"2/runtime/ContainerStatus"},
		},
		{
			name:   "start container 1",
			method: "/runtime.RuntimeService/StartContainer",
			in: &runtimeapi.StartContainerRequest{
				ContainerId: containerId1,
			},
			resp:    &runtimeapi.StartContainerResponse{},
			journal: []string{"1/runtime/StartContainer"},
		},
		{
			name:   "start container 2",
			method: "/runtime.RuntimeService/StartContainer",
			in: &runtimeapi.StartContainerRequest{
				ContainerId: containerId2,
			},
			resp:    &runtimeapi.StartContainerResponse{},
			journal: []string{"2/runtime/StartContainer"},
		},
		{
			name:   "stop container 1",
			method: "/runtime.RuntimeService/StopContainer",
			in: &runtimeapi.StopContainerRequest{
				ContainerId: containerId1,
			},
			resp:    &runtimeapi.StopContainerResponse{},
			journal: []string{"1/runtime/StopContainer"},
		},
		{
			name:   "stop container 2",
			method: "/runtime.RuntimeService/StopContainer",
			in: &runtimeapi.StopContainerRequest{
				ContainerId: containerId2,
			},
			resp:    &runtimeapi.StopContainerResponse{},
			journal: []string{"2/runtime/StopContainer"},
		},
		{
			name:   "remove container 1",
			method: "/runtime.RuntimeService/RemoveContainer",
			in: &runtimeapi.RemoveContainerRequest{
				ContainerId: containerId1,
			},
			resp:    &runtimeapi.RemoveContainerResponse{},
			journal: []string{"1/runtime/RemoveContainer"},
		},
		{
			name:   "remove container 2",
			method: "/runtime.RuntimeService/RemoveContainer",
			in: &runtimeapi.RemoveContainerRequest{
				ContainerId: containerId2,
			},
			resp:    &runtimeapi.RemoveContainerResponse{},
			journal: []string{"2/runtime/RemoveContainer"},
		},
		{
			name:   "exec sync 1",
			method: "/runtime.RuntimeService/ExecSync",
			in: &runtimeapi.ExecSyncRequest{
				ContainerId: containerId1,
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecSyncResponse{ExitCode: 0},
			journal: []string{"1/runtime/ExecSync"},
		},
		{
			name:   "exec sync 2",
			method: "/runtime.RuntimeService/ExecSync",
			in: &runtimeapi.ExecSyncRequest{
				ContainerId: containerId2,
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecSyncResponse{ExitCode: 0},
			journal: []string{"2/runtime/ExecSync"},
		},
		{
			name:   "exec 1",
			method: "/runtime.RuntimeService/Exec",
			in: &runtimeapi.ExecRequest{
				ContainerId: containerId1,
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecResponse{},
			journal: []string{"1/runtime/Exec"},
		},
		{
			name:   "exec 2",
			method: "/runtime.RuntimeService/Exec",
			in: &runtimeapi.ExecRequest{
				ContainerId: containerId2,
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecResponse{},
			journal: []string{"2/runtime/Exec"},
		},
		{
			name:   "attach 1",
			method: "/runtime.RuntimeService/Attach",
			in: &runtimeapi.AttachRequest{
				ContainerId: containerId1,
			},
			resp:    &runtimeapi.AttachResponse{},
			journal: []string{"1/runtime/Attach"},
		},
		{
			name:   "attach 2",
			method: "/runtime.RuntimeService/Attach",
			in: &runtimeapi.AttachRequest{
				ContainerId: containerId2,
			},
			resp:    &runtimeapi.AttachResponse{},
			journal: []string{"2/runtime/Attach"},
		},
		{
			name:   "port forward 1",
			method: "/runtime.RuntimeService/PortForward",
			in: &runtimeapi.PortForwardRequest{
				PodSandboxId: podSandboxId1,
				Port:         []int32{80},
			},
			resp:    &runtimeapi.PortForwardResponse{},
			journal: []string{"1/runtime/PortForward"},
		},
		{
			name:   "port forward 2",
			method: "/runtime.RuntimeService/PortForward",
			in: &runtimeapi.PortForwardRequest{
				PodSandboxId: podSandboxId2,
				Port:         []int32{80},
			},
			resp:    &runtimeapi.PortForwardResponse{},
			journal: []string{"2/runtime/PortForward"},
		},
		{
			name:   "stop pod sandbox 1",
			method: "/runtime.RuntimeService/StopPodSandbox",
			in: &runtimeapi.StopPodSandboxRequest{
				PodSandboxId: podSandboxId1,
			},
			resp:    &runtimeapi.StopPodSandboxResponse{},
			journal: []string{"1/runtime/StopPodSandbox"},
		},
		{
			name:   "stop pod sandbox 2",
			method: "/runtime.RuntimeService/StopPodSandbox",
			in: &runtimeapi.StopPodSandboxRequest{
				PodSandboxId: podSandboxId2,
			},
			resp:    &runtimeapi.StopPodSandboxResponse{},
			journal: []string{"2/runtime/StopPodSandbox"},
		},
		{
			name:   "relist pod sandboxes after stopping",
			method: "/runtime.RuntimeService/ListPodSandbox",
			in:     &runtimeapi.ListPodSandboxRequest{},
			resp: &runtimeapi.ListPodSandboxResponse{
				Items: []*runtimeapi.PodSandbox{
					{
						Id: podSandboxId1,
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      "pod-1-1",
							Uid:       podUid1,
							Namespace: "default",
							Attempt:   0,
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_NOTREADY,
						CreatedAt: tester.servers[0].CurrentTime,
						Labels:    map[string]string{"name": "pod-1-1"},
					},
					{
						Id: podSandboxId2,
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      "pod-2-1",
							Uid:       podUid2,
							Namespace: "default",
							Attempt:   0,
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_NOTREADY,
						CreatedAt: tester.servers[1].CurrentTime,
						Labels:    map[string]string{"name": "pod-2-1"},
						Annotations: map[string]string{
							"kubernetes.io/target-runtime": "alt",
						},
					},
				},
			},
			journal: []string{"1/runtime/ListPodSandbox", "2/runtime/ListPodSandbox"},
		},
		{
			name:   "remove pod sandbox 1",
			method: "/runtime.RuntimeService/RemovePodSandbox",
			in: &runtimeapi.RemovePodSandboxRequest{
				PodSandboxId: podSandboxId1,
			},
			resp:    &runtimeapi.RemovePodSandboxResponse{},
			journal: []string{"1/runtime/RemovePodSandbox"},
		},
		{
			name:   "remove pod sandbox 2",
			method: "/runtime.RuntimeService/RemovePodSandbox",
			in: &runtimeapi.RemovePodSandboxRequest{
				PodSandboxId: podSandboxId2,
			},
			resp:    &runtimeapi.RemovePodSandboxResponse{},
			journal: []string{"2/runtime/RemovePodSandbox"},
		},
		{
			name:    "relist pod sandboxes after removal",
			method:  "/runtime.RuntimeService/ListPodSandbox",
			in:      &runtimeapi.ListPodSandboxRequest{},
			resp:    &runtimeapi.ListPodSandboxResponse{},
			journal: []string{"1/runtime/ListPodSandbox", "2/runtime/ListPodSandbox"},
		},
		{
			name:    "update runtime config",
			method:  "/runtime.RuntimeService/UpdateRuntimeConfig",
			in:      &runtimeapi.UpdateRuntimeConfigRequest{},
			resp:    &runtimeapi.UpdateRuntimeConfigResponse{},
			journal: []string{"1/runtime/UpdateRuntimeConfig", "2/runtime/UpdateRuntimeConfig"},
		},
		{
			name:   "list images",
			method: "/runtime.ImageService/ListImages",
			in:     &runtimeapi.ListImagesRequest{},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       "image1-1",
						RepoTags: []string{"image1-1"},
						Size_:    fakeImageSize1,
					},
					{
						Id:       "image1-2",
						RepoTags: []string{"image1-2"},
						Size_:    fakeImageSize1,
					},
					{
						Id:       "alt/image2-1",
						RepoTags: []string{"alt/image2-1"},
						Size_:    fakeImageSize2,
					},
					{
						Id:       "alt/image2-2",
						RepoTags: []string{"alt/image2-2"},
						Size_:    fakeImageSize2,
					},
				},
			},
			journal: []string{"1/image/ListImages", "2/image/ListImages"},
		},
		{
			name:   "pull image (primary)",
			method: "/runtime.ImageService/PullImage",
			in: &runtimeapi.PullImageRequest{
				Image:         &runtimeapi.ImageSpec{Image: "image1-3"},
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
				Image:         &runtimeapi.ImageSpec{Image: "alt/image2-3"},
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
					Image: &runtimeapi.ImageSpec{Image: "image1-3"},
				},
			},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       "image1-3",
						RepoTags: []string{"image1-3"},
						Size_:    fakeImageSize1,
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
					Image: &runtimeapi.ImageSpec{Image: "alt/image2-3"},
				},
			},
			resp: &runtimeapi.ListImagesResponse{
				Images: []*runtimeapi.Image{
					{
						Id:       "alt/image2-3",
						RepoTags: []string{"alt/image2-3"},
						Size_:    fakeImageSize2,
					},
				},
			},
			journal: []string{"2/image/ListImages"},
		},
		{
			name:   "image status 1-2",
			method: "/runtime.ImageService/ImageStatus",
			in: &runtimeapi.ImageStatusRequest{
				Image: &runtimeapi.ImageSpec{Image: "image1-2"},
			},
			resp: &runtimeapi.ImageStatusResponse{
				Image: &runtimeapi.Image{
					Id:       "image1-2",
					RepoTags: []string{"image1-2"},
					Size_:    fakeImageSize1,
				},
			},
			journal: []string{"1/image/ImageStatus"},
		},
		{
			name:   "image status 2-3",
			method: "/runtime.ImageService/ImageStatus",
			in: &runtimeapi.ImageStatusRequest{
				Image: &runtimeapi.ImageSpec{Image: "alt/image2-3"},
			},
			resp: &runtimeapi.ImageStatusResponse{
				Image: &runtimeapi.Image{
					Id:       "alt/image2-3",
					RepoTags: []string{"alt/image2-3"},
					Size_:    fakeImageSize2,
				},
			},
			journal: []string{"2/image/ImageStatus"},
		},
		{
			name:   "remove image 1-1",
			method: "/runtime.ImageService/RemoveImage",
			in: &runtimeapi.RemoveImageRequest{
				Image: &runtimeapi.ImageSpec{Image: "image1-1"},
			},
			resp:    &runtimeapi.RemoveImageResponse{},
			journal: []string{"1/image/RemoveImage"},
		},
		{
			name:   "remove image 2-2",
			method: "/runtime.ImageService/RemoveImage",
			in: &runtimeapi.RemoveImageRequest{
				Image: &runtimeapi.ImageSpec{Image: "alt/image2-2"},
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
						Id:       "image1-2",
						RepoTags: []string{"image1-2"},
						Size_:    fakeImageSize1,
					},
					{
						Id:       "image1-3",
						RepoTags: []string{"image1-3"},
						Size_:    fakeImageSize1,
					},
					{
						Id:       "alt/image2-1",
						RepoTags: []string{"alt/image2-1"},
						Size_:    fakeImageSize2,
					},
					{
						Id:       "alt/image2-3",
						RepoTags: []string{"alt/image2-3"},
						Size_:    fakeImageSize2,
					},
				},
			},
			journal: []string{"1/image/ListImages", "2/image/ListImages"},
		},
	}

	nCalls := 0
	for _, step := range testCases {
		var ins []interface{}
		if step.ins == nil {
			ins = []interface{}{step.in}
		} else {
			if step.in != nil {
				t.Fatalf("can't specify both 'in' and 'ins' for the step %s", step.name)
			}
			ins = step.ins
		}

		for n, in := range ins {
			name := step.name
			if len(ins) > 1 {
				name = fmt.Sprintf("%s [%d]", name, n+1)
			}
			nCalls++
			t.Run(name, func(t *testing.T) {
				tester.verifyCall(t, step.method, in, step.resp, step.error)
				tester.verifyJournal(t, step.journal)
			})
		}
	}
	if tester.hookCallCount != nCalls {
		t.Errorf("unexpected hook call count: %d instead of %d", tester.hookCallCount, nCalls)
	}
}

func TestCriProxyNoStartupRace(t *testing.T) {
	tester := newProxyTester(t)
	defer tester.stop()
	tester.startServers(t, 0)

	tester.startProxy(t)
	tester.connectToProxy(t)
	// should not need 2nd runtime to contact just the first one
	listReq := &runtimeapi.ListImagesRequest{
		Filter: &runtimeapi.ImageFilter{
			Image: &runtimeapi.ImageSpec{Image: "image1-2"},
		},
	}

	// this one skips 2nd client because of the filter
	tester.verifyCall(t, "/runtime.ImageService/ListImages", listReq, &runtimeapi.ListImagesResponse{
		Images: []*runtimeapi.Image{
			{
				Id:       "image1-2",
				RepoTags: []string{"image1-2"},
				Size_:    fakeImageSize1,
			},
		},
	}, "")
	tester.verifyJournal(t, []string{"1/image/ListImages"})

	// this one skips 2nd client because it's not connected yet
	tester.verifyCall(t, "/runtime.ImageService/ListImages", &runtimeapi.ListImagesRequest{}, &runtimeapi.ListImagesResponse{
		Images: []*runtimeapi.Image{
			{
				Id:       "image1-1",
				RepoTags: []string{"image1-1"},
				Size_:    fakeImageSize1,
			},
			{
				Id:       "image1-2",
				RepoTags: []string{"image1-2"},
				Size_:    fakeImageSize1,
			},
		},
	}, "")
	tester.verifyJournal(t, []string{"1/image/ListImages"})

	tester.verifyCall(t, "/runtime.RuntimeService/UpdateRuntimeConfig", &runtimeapi.UpdateRuntimeConfigRequest{}, &runtimeapi.UpdateRuntimeConfigResponse{}, "")
	tester.verifyJournal(t, []string{"1/runtime/UpdateRuntimeConfig"})

	tester.startServers(t, 1)

	for i := 0; ; i++ {
		if i == 100 {
			t.Fatalf("2nd client didn't activate")
		}
		time.Sleep(200 * time.Millisecond)
		var resp runtimeapi.ListImagesResponse
		if err := tester.invoke("/runtime.ImageService/ListImages", &runtimeapi.ListImagesRequest{}, &resp); err != nil {
			t.Fatalf("ListImages() failed while waiting for 2nd client to connect: %v", err)
		}
		if len(resp.GetImages()) == 4 {
			tester.verifyJournal(t, []string{"1/image/ListImages", "2/image/ListImages"})
			break
		} else {
			tester.verifyJournal(t, []string{"1/image/ListImages"})
		}
	}

	tester.verifyCall(t, "/runtime.RuntimeService/UpdateRuntimeConfig", &runtimeapi.UpdateRuntimeConfigRequest{}, &runtimeapi.UpdateRuntimeConfigResponse{}, "")
	tester.verifyJournal(t, []string{"1/runtime/UpdateRuntimeConfig", "2/runtime/UpdateRuntimeConfig"})

	tester.verifyCall(t, "/runtime.ImageService/ListImages", &runtimeapi.ListImagesRequest{}, &runtimeapi.ListImagesResponse{
		Images: []*runtimeapi.Image{
			{
				Id:       "image1-1",
				RepoTags: []string{"image1-1"},
				Size_:    fakeImageSize1,
			},
			{
				Id:       "image1-2",
				RepoTags: []string{"image1-2"},
				Size_:    fakeImageSize1,
			},
			{
				Id:       "alt/image2-1",
				RepoTags: []string{"alt/image2-1"},
				Size_:    fakeImageSize2,
			},
			{
				Id:       "alt/image2-2",
				RepoTags: []string{"alt/image2-2"},
				Size_:    fakeImageSize2,
			},
		},
	}, "")
	tester.verifyJournal(t, []string{"1/image/ListImages", "2/image/ListImages"})
}

func init() {
	// FIXME: testing.Verbose() always returns false
	flag.Set("alsologtostderr", "true")
	flag.Set("v", "5")
}

// TODO: never wait for the client to connect, just err
// TODO: proper status handling (contact both runtimes, etc.)
// TODO: make sure patching requests/responses is ok & if it is, don't use copying for them
