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
	"strings"
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
	journal *proxytest.SimpleJournal
	servers []*proxytest.FakeCriServer
	proxy   *RuntimeProxy
	conn    *grpc.ClientConn
}

func newProxyTester() *proxyTester {
	journal := proxytest.NewSimpleJournal()
	return &proxyTester{
		journal: journal,
		servers: []*proxytest.FakeCriServer{
			proxytest.NewFakeCriServer(proxytest.NewPrefixJournal(journal, "1/")),
			proxytest.NewFakeCriServer(proxytest.NewPrefixJournal(journal, "2/")),
		},
		proxy: NewRuntimeProxy([]string{fakeCriSocketPath1, altSocketSpec}, connectionTimeoutForTests),
	}
}

func (tester *proxyTester) startServers(t *testing.T) {
	startServer(t, tester.servers[0], fakeCriSocketPath1)
	startServer(t, tester.servers[1], fakeCriSocketPath2)
}

func (tester *proxyTester) startProxy(t *testing.T) {
	if err := tester.proxy.Connect(); err != nil {
		t.Fatalf("Failed to set up CRI proxy: %v", err)
	}
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

func (tester *proxyTester) invoke(method string, in, resp interface{}) error {
	return grpc.Invoke(context.Background(), method, in, resp, tester.conn)
}

func (tester *proxyTester) verifyJournal(t *testing.T, expectedJournal []string) {
	if err := tester.journal.Verify(expectedJournal); err != nil {
		t.Error(err)
	}
}

func TestCriProxy(t *testing.T) {
	tester := newProxyTester()
	defer tester.stop()
	tester.startServers(t)
	tester.startProxy(t)
	tester.connectToProxy(t)

	fakeImageNames1 := []string{"image1-1", "image1-2"}
	tester.servers[0].SetFakeImages(fakeImageNames1)
	tester.servers[0].SetFakeImageSize(fakeImageSize1)

	fakeImageNames2 := []string{"image2-1", "image2-2"}
	tester.servers[1].SetFakeImages(fakeImageNames2)
	tester.servers[1].SetFakeImageSize(fakeImageSize2)

	for _, step := range []struct {
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
			name:   "run pod sandbox 1",
			method: "/runtime.RuntimeService/RunPodSandbox",
			in: &runtimeapi.RunPodSandboxRequest{
				Config: &runtimeapi.PodSandboxConfig{
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      pstr("pod-1-1"),
						Uid:       pstr(podUid1),
						Namespace: pstr("default"),
						Attempt:   puint32(0),
					},
					Labels: map[string]string{"name": "pod-1-1"},
				},
			},
			resp: &runtimeapi.RunPodSandboxResponse{
				PodSandboxId: pstr(podSandboxId1),
			},
			journal: []string{"1/runtime/RunPodSandbox"},
		},
		{
			name:   "run pod sandbox 2",
			method: "/runtime.RuntimeService/RunPodSandbox",
			in: &runtimeapi.RunPodSandboxRequest{
				Config: &runtimeapi.PodSandboxConfig{
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      pstr("pod-2-1"),
						Uid:       pstr(podUid2),
						Namespace: pstr("default"),
						Attempt:   puint32(0),
					},
					Labels: map[string]string{"name": "pod-2-1"},
					Annotations: map[string]string{
						"kubernetes.io/target-runtime": "alt",
					},
				},
			},
			resp: &runtimeapi.RunPodSandboxResponse{
				PodSandboxId: pstr(podSandboxId2),
			},
			journal: []string{"2/runtime/RunPodSandbox"},
		},
		{
			name:   "run pod sandbox with bad runtime id",
			method: "/runtime.RuntimeService/RunPodSandbox",
			in: &runtimeapi.RunPodSandboxRequest{
				Config: &runtimeapi.PodSandboxConfig{
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      pstr("pod-x-1"),
						Uid:       pstr(podUid2),
						Namespace: pstr("default"),
						Attempt:   puint32(0),
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
						Id: pstr(podSandboxId1),
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      pstr("pod-1-1"),
							Uid:       pstr(podUid1),
							Namespace: pstr("default"),
							Attempt:   puint32(0),
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY.Enum(),
						CreatedAt: &tester.servers[0].CurrentTime,
						Labels:    map[string]string{"name": "pod-1-1"},
					},
					{
						Id: pstr(podSandboxId2),
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      pstr("pod-2-1"),
							Uid:       pstr(podUid2),
							Namespace: pstr("default"),
							Attempt:   puint32(0),
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY.Enum(),
						CreatedAt: &tester.servers[1].CurrentTime,
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
				Filter: &runtimeapi.PodSandboxFilter{Id: pstr(podSandboxId1)},
			},
			resp: &runtimeapi.ListPodSandboxResponse{
				Items: []*runtimeapi.PodSandbox{
					{
						Id: pstr(podSandboxId1),
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      pstr("pod-1-1"),
							Uid:       pstr(podUid1),
							Namespace: pstr("default"),
							Attempt:   puint32(0),
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY.Enum(),
						CreatedAt: &tester.servers[0].CurrentTime,
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
				Filter: &runtimeapi.PodSandboxFilter{Id: pstr(podSandboxId2)},
			},
			resp: &runtimeapi.ListPodSandboxResponse{
				Items: []*runtimeapi.PodSandbox{
					{
						Id: pstr(podSandboxId2),
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      pstr("pod-2-1"),
							Uid:       pstr(podUid2),
							Namespace: pstr("default"),
							Attempt:   puint32(0),
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_READY.Enum(),
						CreatedAt: &tester.servers[1].CurrentTime,
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
				PodSandboxId: pstr(podSandboxId1),
			},
			resp: &runtimeapi.PodSandboxStatusResponse{
				Status: &runtimeapi.PodSandboxStatus{
					Id: pstr(podSandboxId1),
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      pstr("pod-1-1"),
						Uid:       pstr(podUid1),
						Namespace: pstr("default"),
						Attempt:   puint32(0),
					},
					State:     runtimeapi.PodSandboxState_SANDBOX_READY.Enum(),
					CreatedAt: &tester.servers[0].CurrentTime,
					Network: &runtimeapi.PodSandboxNetworkStatus{
						Ip: pstr("192.168.192.168"),
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
				PodSandboxId: pstr(podSandboxId2),
			},
			resp: &runtimeapi.PodSandboxStatusResponse{
				Status: &runtimeapi.PodSandboxStatus{
					Id: pstr(podSandboxId2),
					Metadata: &runtimeapi.PodSandboxMetadata{
						Name:      pstr("pod-2-1"),
						Uid:       pstr(podUid2),
						Namespace: pstr("default"),
						Attempt:   puint32(0),
					},
					State:     runtimeapi.PodSandboxState_SANDBOX_READY.Enum(),
					CreatedAt: &tester.servers[1].CurrentTime,
					Network: &runtimeapi.PodSandboxNetworkStatus{
						Ip: pstr("192.168.192.168"),
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
				PodSandboxId: pstr(podSandboxId1),
				Config: &runtimeapi.ContainerConfig{
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    pstr("container1"),
						Attempt: puint32(0),
					},
					Image: &runtimeapi.ImageSpec{
						Image: pstr("image1-1"),
					},
				},
			},
			resp: &runtimeapi.CreateContainerResponse{
				ContainerId: pstr(containerId1),
			},
			journal: []string{"1/runtime/CreateContainer"},
		},
		{
			name:   "create container 2",
			method: "/runtime.RuntimeService/CreateContainer",
			in: &runtimeapi.CreateContainerRequest{
				PodSandboxId: pstr(podSandboxId2),
				Config: &runtimeapi.ContainerConfig{
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    pstr("container2"),
						Attempt: puint32(0),
					},
					Image: &runtimeapi.ImageSpec{
						Image: pstr("alt/image2-1"),
					},
				},
			},
			resp: &runtimeapi.CreateContainerResponse{
				ContainerId: pstr(containerId2),
			},
			journal: []string{"2/runtime/CreateContainer"},
		},
		{
			name:   "try to create container for a wrong runtime",
			method: "/runtime.RuntimeService/CreateContainer",
			in: &runtimeapi.CreateContainerRequest{
				PodSandboxId: pstr(podSandboxId2),
				Config: &runtimeapi.ContainerConfig{
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    pstr("container2"),
						Attempt: puint32(0),
					},
					Image: &runtimeapi.ImageSpec{
						Image: pstr("image1-2"),
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
						Id:           pstr(containerId1),
						PodSandboxId: pstr(podSandboxId1),
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    pstr("container1"),
							Attempt: puint32(0),
						},
						Image: &runtimeapi.ImageSpec{
							Image: pstr("image1-1"),
						},
						ImageRef:  pstr("image1-1"),
						CreatedAt: &tester.servers[0].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
					},
					{
						Id:           pstr(containerId2),
						PodSandboxId: pstr(podSandboxId2),
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    pstr("container2"),
							Attempt: puint32(0),
						},
						Image: &runtimeapi.ImageSpec{
							Image: pstr("alt/image2-1"),
						},
						ImageRef:  pstr("image2-1"),
						CreatedAt: &tester.servers[1].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
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
					Filter: &runtimeapi.ContainerFilter{Id: pstr(containerId1)},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{PodSandboxId: pstr(podSandboxId1)},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           pstr(containerId1),
						PodSandboxId: pstr(podSandboxId1),
					},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           pstr(containerId1),
						PodSandboxId: pstr(podSandboxId1),
						State:        runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
					},
				},
			},
			resp: &runtimeapi.ListContainersResponse{
				Containers: []*runtimeapi.Container{
					{
						Id:           pstr(containerId1),
						PodSandboxId: pstr(podSandboxId1),
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    pstr("container1"),
							Attempt: puint32(0),
						},
						Image: &runtimeapi.ImageSpec{
							Image: pstr("image1-1"),
						},
						ImageRef:  pstr("image1-1"),
						CreatedAt: &tester.servers[0].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
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
					Filter: &runtimeapi.ContainerFilter{Id: pstr(containerId2)},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{PodSandboxId: pstr(podSandboxId2)},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           pstr(containerId2),
						PodSandboxId: pstr(podSandboxId2),
					},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           pstr(containerId2),
						PodSandboxId: pstr(podSandboxId2),
						State:        runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
					},
				},
			},
			resp: &runtimeapi.ListContainersResponse{
				Containers: []*runtimeapi.Container{
					{
						Id:           pstr(containerId2),
						PodSandboxId: pstr(podSandboxId2),
						Metadata: &runtimeapi.ContainerMetadata{
							Name:    pstr("container2"),
							Attempt: puint32(0),
						},
						Image: &runtimeapi.ImageSpec{
							Image: pstr("alt/image2-1"),
						},
						ImageRef:  pstr("image2-1"),
						CreatedAt: &tester.servers[1].CurrentTime,
						State:     runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
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
						Id:           pstr(containerId1),
						PodSandboxId: pstr(podSandboxId2),
					},
				},
				&runtimeapi.ListContainersRequest{
					Filter: &runtimeapi.ContainerFilter{
						Id:           pstr(containerId1),
						PodSandboxId: pstr(podSandboxId2),
						State:        runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
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
				ContainerId: pstr(containerId1),
			},
			resp: &runtimeapi.ContainerStatusResponse{
				Status: &runtimeapi.ContainerStatus{
					Id: pstr(containerId1),
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    pstr("container1"),
						Attempt: puint32(0),
					},
					Image: &runtimeapi.ImageSpec{
						Image: pstr("image1-1"),
					},
					ImageRef:  pstr("image1-1"),
					CreatedAt: &tester.servers[0].CurrentTime,
					State:     runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
				},
			},
			journal: []string{"1/runtime/ContainerStatus"},
		},
		{
			name:   "container status 2",
			method: "/runtime.RuntimeService/ContainerStatus",
			in: &runtimeapi.ContainerStatusRequest{
				ContainerId: pstr(containerId2),
			},
			resp: &runtimeapi.ContainerStatusResponse{
				Status: &runtimeapi.ContainerStatus{
					Id: pstr(containerId2),
					Metadata: &runtimeapi.ContainerMetadata{
						Name:    pstr("container2"),
						Attempt: puint32(0),
					},
					Image: &runtimeapi.ImageSpec{
						Image: pstr("alt/image2-1"),
					},
					// ImageRef is not prefixed
					ImageRef:  pstr("image2-1"),
					CreatedAt: &tester.servers[1].CurrentTime,
					State:     runtimeapi.ContainerState_CONTAINER_CREATED.Enum(),
				},
			},
			journal: []string{"2/runtime/ContainerStatus"},
		},
		{
			name:   "start container 1",
			method: "/runtime.RuntimeService/StartContainer",
			in: &runtimeapi.StartContainerRequest{
				ContainerId: pstr(containerId1),
			},
			resp:    &runtimeapi.StartContainerResponse{},
			journal: []string{"1/runtime/StartContainer"},
		},
		{
			name:   "start container 2",
			method: "/runtime.RuntimeService/StartContainer",
			in: &runtimeapi.StartContainerRequest{
				ContainerId: pstr(containerId2),
			},
			resp:    &runtimeapi.StartContainerResponse{},
			journal: []string{"2/runtime/StartContainer"},
		},
		{
			name:   "stop container 1",
			method: "/runtime.RuntimeService/StopContainer",
			in: &runtimeapi.StopContainerRequest{
				ContainerId: pstr(containerId1),
			},
			resp:    &runtimeapi.StopContainerResponse{},
			journal: []string{"1/runtime/StopContainer"},
		},
		{
			name:   "stop container 2",
			method: "/runtime.RuntimeService/StopContainer",
			in: &runtimeapi.StopContainerRequest{
				ContainerId: pstr(containerId2),
			},
			resp:    &runtimeapi.StopContainerResponse{},
			journal: []string{"2/runtime/StopContainer"},
		},
		{
			name:   "remove container 1",
			method: "/runtime.RuntimeService/RemoveContainer",
			in: &runtimeapi.RemoveContainerRequest{
				ContainerId: pstr(containerId1),
			},
			resp:    &runtimeapi.RemoveContainerResponse{},
			journal: []string{"1/runtime/RemoveContainer"},
		},
		{
			name:   "remove container 2",
			method: "/runtime.RuntimeService/RemoveContainer",
			in: &runtimeapi.RemoveContainerRequest{
				ContainerId: pstr(containerId2),
			},
			resp:    &runtimeapi.RemoveContainerResponse{},
			journal: []string{"2/runtime/RemoveContainer"},
		},
		{
			name:   "exec sync 1",
			method: "/runtime.RuntimeService/ExecSync",
			in: &runtimeapi.ExecSyncRequest{
				ContainerId: pstr(containerId1),
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecSyncResponse{ExitCode: pint32(0)},
			journal: []string{"1/runtime/ExecSync"},
		},
		{
			name:   "exec sync 2",
			method: "/runtime.RuntimeService/ExecSync",
			in: &runtimeapi.ExecSyncRequest{
				ContainerId: pstr(containerId2),
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecSyncResponse{ExitCode: pint32(0)},
			journal: []string{"2/runtime/ExecSync"},
		},
		{
			name:   "exec 1",
			method: "/runtime.RuntimeService/Exec",
			in: &runtimeapi.ExecRequest{
				ContainerId: pstr(containerId1),
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecResponse{},
			journal: []string{"1/runtime/Exec"},
		},
		{
			name:   "exec 2",
			method: "/runtime.RuntimeService/Exec",
			in: &runtimeapi.ExecRequest{
				ContainerId: pstr(containerId2),
				Cmd:         []string{"ls"},
			},
			resp:    &runtimeapi.ExecResponse{},
			journal: []string{"2/runtime/Exec"},
		},
		{
			name:   "attach 1",
			method: "/runtime.RuntimeService/Attach",
			in: &runtimeapi.AttachRequest{
				ContainerId: pstr(containerId1),
			},
			resp:    &runtimeapi.AttachResponse{},
			journal: []string{"1/runtime/Attach"},
		},
		{
			name:   "attach 2",
			method: "/runtime.RuntimeService/Attach",
			in: &runtimeapi.AttachRequest{
				ContainerId: pstr(containerId2),
			},
			resp:    &runtimeapi.AttachResponse{},
			journal: []string{"2/runtime/Attach"},
		},
		{
			name:   "port forward 1",
			method: "/runtime.RuntimeService/PortForward",
			in: &runtimeapi.PortForwardRequest{
				PodSandboxId: pstr(podSandboxId1),
				Port:         []int32{80},
			},
			resp:    &runtimeapi.PortForwardResponse{},
			journal: []string{"1/runtime/PortForward"},
		},
		{
			name:   "port forward 2",
			method: "/runtime.RuntimeService/PortForward",
			in: &runtimeapi.PortForwardRequest{
				PodSandboxId: pstr(podSandboxId2),
				Port:         []int32{80},
			},
			resp:    &runtimeapi.PortForwardResponse{},
			journal: []string{"2/runtime/PortForward"},
		},
		{
			name:   "stop pod sandbox 1",
			method: "/runtime.RuntimeService/StopPodSandbox",
			in: &runtimeapi.StopPodSandboxRequest{
				PodSandboxId: pstr(podSandboxId1),
			},
			resp:    &runtimeapi.StopPodSandboxResponse{},
			journal: []string{"1/runtime/StopPodSandbox"},
		},
		{
			name:   "stop pod sandbox 2",
			method: "/runtime.RuntimeService/StopPodSandbox",
			in: &runtimeapi.StopPodSandboxRequest{
				PodSandboxId: pstr(podSandboxId2),
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
						Id: pstr(podSandboxId1),
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      pstr("pod-1-1"),
							Uid:       pstr(podUid1),
							Namespace: pstr("default"),
							Attempt:   puint32(0),
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_NOTREADY.Enum(),
						CreatedAt: &tester.servers[0].CurrentTime,
						Labels:    map[string]string{"name": "pod-1-1"},
					},
					{
						Id: pstr(podSandboxId2),
						Metadata: &runtimeapi.PodSandboxMetadata{
							Name:      pstr("pod-2-1"),
							Uid:       pstr(podUid2),
							Namespace: pstr("default"),
							Attempt:   puint32(0),
						},
						State:     runtimeapi.PodSandboxState_SANDBOX_NOTREADY.Enum(),
						CreatedAt: &tester.servers[1].CurrentTime,
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
				PodSandboxId: pstr(podSandboxId1),
			},
			resp:    &runtimeapi.RemovePodSandboxResponse{},
			journal: []string{"1/runtime/RemovePodSandbox"},
		},
		{
			name:   "remove pod sandbox 2",
			method: "/runtime.RuntimeService/RemovePodSandbox",
			in: &runtimeapi.RemovePodSandboxRequest{
				PodSandboxId: pstr(podSandboxId2),
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
			t.Run(name, func(t *testing.T) {
				actualResponse := reflect.New(reflect.TypeOf(step.resp).Elem()).Interface()
				err := tester.invoke(step.method, in, actualResponse)
				switch {
				case step.error == "" && err != nil:
					t.Errorf("GRPC call failed: %v", err)
				case step.error != "" && err == nil:
					t.Errorf("did not get expected error")
				case step.error != "" && !strings.Contains(err.Error(), step.error):
					t.Errorf("bad error message: %q instead of %q", err.Error(), step.error)
				}

				if err == nil && !reflect.DeepEqual(actualResponse, step.resp) {
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

				tester.verifyJournal(t, step.journal)
			})
		}
	}
}

func TestCriProxyNoStartupRace(t *testing.T) {
	tester := newProxyTester()
	defer tester.stop()
	go func() {
		time.Sleep(500 * time.Millisecond)
		tester.startServers(t)
	}()

	tester.startProxy(t)
	tester.connectToProxy(t)
	if err := tester.invoke("/runtime.RuntimeService/UpdateRuntimeConfig", &runtimeapi.UpdateRuntimeConfigRequest{}, &runtimeapi.UpdateRuntimeConfigResponse{}); err != nil {
		t.Errorf("failed to invoke UpdateRuntimeConfig(): %v", err)
	}
}

// TODO: proper status handling (contact both runtimes, etc.)
// TODO: make sure patching requests/responses is ok & if it is, don't use copying for them
