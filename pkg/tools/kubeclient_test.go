/*
Copyright 2018 Mirantis

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

package tools

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/pkg/api"
	v1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	fakerest "k8s.io/client-go/rest/fake"
	testcore "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	sampleContainerID   = "docker://virtlet.cloud__2232e3bf-d702-5824-5e3c-f12e60e616b0"
	portForwardWaitTime = 1 * time.Minute
)

type fakeExecutor struct {
	t             *testing.T
	called        bool
	config        *rest.Config
	method        string
	url           *url.URL
	streamOptions *remotecommand.StreamOptions
}

var _ remoteExecutor = &fakeExecutor{}

func (e *fakeExecutor) stream(config *rest.Config, method string, url *url.URL, options remotecommand.StreamOptions) error {
	if e.called {
		e.t.Errorf("Stream called twice")
	}
	e.called = true
	e.config = config
	e.method = method
	e.url = url
	e.streamOptions = &options
	return nil
}

func parsePort(portStr string) (uint16, uint16, error) {
	var localStr, remoteStr string
	parts := strings.Split(portStr, ":")
	switch {
	case len(parts) == 1:
		localStr = parts[0]
		remoteStr = parts[0]
	case len(parts) == 2:
		localStr = parts[0]
		if localStr == "" {
			localStr = "0"
		}
		remoteStr = parts[1]
	default:
		return 0, 0, fmt.Errorf("invalid port string %q", portStr)
	}

	localPort, err := strconv.ParseUint(localStr, 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("bad local port string %q", localStr)
	}

	remotePort, err := strconv.ParseUint(remoteStr, 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("bad remtoe port string %q", remoteStr)
	}

	if remotePort == 0 {
		return 0, 0, fmt.Errorf("remote port must not be zero")
	}

	return uint16(localPort), uint16(remotePort), nil
}

type fakePortForwarder struct {
	t      *testing.T
	called bool
	config *rest.Config
	method string
	url    *url.URL
	ports  string
}

var _ portForwarder = &fakePortForwarder{}

func (pf *fakePortForwarder) forwardPorts(config *rest.Config, method string, url *url.URL, ports []string, stopChannel, readyChannel chan struct{}, out io.Writer) error {
	if pf.called {
		pf.t.Errorf("ForwardPorts called twice")
	}
	pf.called = true
	pf.config = config
	pf.method = method
	pf.url = url
	pf.ports = strings.Join(ports, " ")
	if readyChannel != nil {
		close(readyChannel)
	}
	for n, portStr := range ports {
		localPort, remotePort, err := parsePort(portStr)
		if err != nil {
			return err
		}
		if localPort == 0 {
			// "random" local port https://xkcd.com/221/
			localPort = 4242 + uint16(n)
		}
		fmt.Fprintf(out, "Forwarding from 127.0.0.1:%d -> %d\n", localPort, remotePort)
	}
	if stopChannel == nil {
		pf.t.Errorf("no stop channel set")
	} else {
		select {
		case <-stopChannel:
		case <-time.After(portForwardWaitTime):
			pf.t.Errorf("timed out waiting for the port forwarder to stop")
		}
	}
	return nil
}

func TestGetVirtletPodNames(t *testing.T) {
	fc := &fakekube.Clientset{}
	fc.AddReactor("list", "pods", func(action testcore.Action) (bool, runtime.Object, error) {
		expectedNamespace := "kube-system"
		if action.GetNamespace() != expectedNamespace {
			t.Errorf("wrong namespace: %q instead of %q", action.GetNamespace(), expectedNamespace)
		}
		return true, &v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: meta_v1.ObjectMeta{
						Name:      "virtlet-foo42",
						Namespace: "kube-system",
						Labels: map[string]string{
							"runtime": "virtlet",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "kube-node-1",
					},
				},
				{
					ObjectMeta: meta_v1.ObjectMeta{
						Name:      "virtlet-g9wtz",
						Namespace: "kube-system",
						Labels: map[string]string{
							"runtime": "virtlet",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "kube-node-2",
					},
				},
				// this pod doesn't have proper labels and thus
				// it should be ignored
				{
					ObjectMeta: meta_v1.ObjectMeta{
						Name:      "whatever",
						Namespace: "kube-system",
					},
				},
			},
		}, nil
	})

	c := &RealKubeClient{client: fc}
	podNames, nodeNames, err := c.GetVirtletPodAndNodeNames()
	if err != nil {
		t.Fatalf("GetVirtletPodNames(): %v", err)
	}
	podNamesStr := strings.Join(podNames, ",")
	expectedPodNamesStr := "virtlet-foo42,virtlet-g9wtz"
	if podNamesStr != expectedPodNamesStr {
		t.Errorf("Bad pod names: %q instead of %q", podNamesStr, expectedPodNamesStr)
	}
	nodeNamesStr := strings.Join(nodeNames, ",")
	expectedNodeNamesStr := "kube-node-1,kube-node-2"
	if nodeNamesStr != expectedNodeNamesStr {
		t.Errorf("Bad pod names: %q instead of %q", podNamesStr, expectedPodNamesStr)
	}
}

func TestGetVMPodInfo(t *testing.T) {
	fc := &fakekube.Clientset{}
	fc.AddReactor("get", "pods", func(action testcore.Action) (bool, runtime.Object, error) {
		expectedNamespace := "default"
		if action.GetNamespace() != expectedNamespace {
			t.Errorf("Wrong namespace: %q instead of %q", action.GetNamespace(), expectedNamespace)
		}
		getAction := action.(testcore.GetAction)
		expectedName := "cirros-vm"
		if getAction.GetName() != expectedName {
			t.Errorf("Bad pod name: %q instead of %q", getAction.GetName(), expectedName)
		}
		return true, &v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "cirros-vm",
				Namespace: "default",
				Annotations: map[string]string{
					"kubernetes.io/target-runtime": "virtlet.cloud",
				},
			},
			Spec: v1.PodSpec{
				NodeName: "kube-node-1",
				Containers: []v1.Container{
					{
						Name: "foocontainer",
					},
				},
			},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:        "foocontainer",
						ContainerID: sampleContainerID,
					},
				},
			},
		}, nil
	})
	fc.AddReactor("list", "pods", func(action testcore.Action) (bool, runtime.Object, error) {
		expectedNamespace := "kube-system"
		if action.GetNamespace() != expectedNamespace {
			t.Errorf("wrong namespace: %q instead of %q", action.GetNamespace(), expectedNamespace)
		}
		// fake Clientset doesn't handle the field selector currently
		listAction := action.(testcore.ListAction)
		expectedFieldSelector := "spec.nodeName=kube-node-1"
		fieldSelector := listAction.GetListRestrictions().Fields.String()
		if fieldSelector != expectedFieldSelector {
			t.Errorf("bad fieldSelector: %q instead of %q", fieldSelector, expectedFieldSelector)
		}
		return true, &v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: meta_v1.ObjectMeta{
						Name:      "virtlet-g9wtz",
						Namespace: "kube-system",
						Labels: map[string]string{
							"runtime": "virtlet",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "kube-node-1",
					},
				},
				// this pod doesn't have proper labels and thus
				// it should be ignored
				{
					ObjectMeta: meta_v1.ObjectMeta{
						Name:      "whatever",
						Namespace: "kube-system",
					},
					Spec: v1.PodSpec{
						NodeName: "kube-node-1",
					},
				},
			},
		}, nil
	})

	c := &RealKubeClient{client: fc, namespace: "default"}
	vmPodInfo, err := c.GetVMPodInfo("cirros-vm")
	if err != nil {
		t.Fatalf("GetVirtletPodNames(): %v", err)
	}

	expectedVMPodInfo := &VMPodInfo{
		NodeName:       "kube-node-1",
		VirtletPodName: "virtlet-g9wtz",
		ContainerID:    sampleContainerID,
		ContainerName:  "foocontainer",
	}
	if !reflect.DeepEqual(expectedVMPodInfo, vmPodInfo) {
		t.Errorf("Bad VM PodInfo: got:\n%s\ninstead of\n%s", spew.Sdump(vmPodInfo), spew.Sdump(expectedVMPodInfo))
	}

	expectedDomainName := "virtlet-2232e3bf-d702-foocontainer"
	if vmPodInfo.LibvirtDomainName() != expectedDomainName {
		t.Errorf("Bad libvirt domain name: %q instead of %q", vmPodInfo.LibvirtDomainName(), expectedDomainName)
	}
}

func TestCheckForVMPod(t *testing.T) {
	for _, tc := range []struct {
		name        string
		annotations map[string]string
	}{
		{
			name:        "no annotations",
			annotations: nil,
		},
		{
			name: "wrong annotation",
			annotations: map[string]string{
				"kubernetes.io/target-runtime": "foobar",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakekube.Clientset{}
			fc.AddReactor("get", "pods", func(action testcore.Action) (bool, runtime.Object, error) {
				return true, &v1.Pod{
					ObjectMeta: meta_v1.ObjectMeta{
						Name:        "cirros-vm",
						Namespace:   "default",
						Annotations: tc.annotations,
					},
					Spec: v1.PodSpec{
						NodeName: "kube-node-1",
						Containers: []v1.Container{
							{
								Name: "foocontainer",
							},
						},
					},
					Status: v1.PodStatus{
						ContainerStatuses: []v1.ContainerStatus{
							{
								Name:        "foocontainer",
								ContainerID: sampleContainerID,
							},
						},
					},
				}, nil
			})

			c := &RealKubeClient{client: fc, namespace: "default"}
			switch _, err := c.GetVMPodInfo("cirros-vm"); {
			case err == nil:
				t.Errorf("didn't get an expected error for a pod w/o Virtlet runtime annotation")
			case !strings.Contains(err.Error(), "annotation"):
				t.Errorf("wrong error message for a pod w/o Virtlet runtime annotation: %q", err)
			}
		})
	}
}

func TestExecInContainer(t *testing.T) {
	restClient := &fakerest.RESTClient{
		// NOTE: APIRegistry will no longer be necessary in newer client-go
		APIRegistry: api.Registry,
		NegotiatedSerializer://testapi.Default.NegotiatedSerializer(),
		dynamic.ContentConfig().NegotiatedSerializer,
		Client: fakerest.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			// this handler is not actually invoked
			return nil, nil
		}),
	}
	config := &rest.Config{Host: "foo-host"}
	fe := &fakeExecutor{t: t}
	c := &RealKubeClient{
		client:     &fakekube.Clientset{},
		config:     config,
		restClient: restClient,
		executor:   fe,
	}
	var stdin, stdout, stderr bytes.Buffer
	exitCode, err := c.ExecInContainer("virtlet-foo42", "virtlet", "kube-system", &stdin, &stdout, &stderr, []string{"echo", "foobar"})
	if err != nil {
		t.Errorf("ExecInContainer returned error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("ExecInContainer returned non-zero exit code %v", exitCode)
	}
	if fe.config == nil {
		t.Errorf("fe.config not set")
	} else if fe.config.Host != "foo-host" {
		t.Errorf("Bad host in rest config: %q instead of foo-host", fe.config.Host)
	}
	if fe.method != "POST" {
		t.Errorf("Bad method %q instead of POST", fe.method)
	}
	expectedPath := "/namespaces/kube-system/pods/virtlet-foo42/exec"
	if fe.url == nil {
		t.Errorf("fe.url not set")
	} else if fe.url.Path != expectedPath {
		t.Errorf("Bad expectedPath: %q instead of %q", fe.url.Path, expectedPath)
	}

	if fe.streamOptions == nil {
		t.Errorf("StreamOptions not set (perhaps Stream() not called)")
	} else {
		if fe.streamOptions.Stdin != &stdin {
			t.Errorf("Bad stdin")
		}
		if fe.streamOptions.Stdout != &stdout {
			t.Errorf("Bad stdout")
		}
		if fe.streamOptions.Stderr != &stderr {
			t.Errorf("Bad stderr")
		}
	}

	expectedValues := url.Values{
		"command":   {"echo", "foobar"},
		"container": {"virtlet"},
		"stderr":    {"true"},
		"stdin":     {"true"},
		"stdout":    {"true"},
	}
	if !reflect.DeepEqual(expectedValues, fe.url.Query()) {
		t.Errorf("Bad query: %#v", fe.url.Query())
	}
}

func TestPortForward(t *testing.T) {
	fc := &fakekube.Clientset{}
	fc.AddReactor("get", "pods", func(action testcore.Action) (bool, runtime.Object, error) {
		expectedNamespace := "kube-system"
		if action.GetNamespace() != expectedNamespace {
			t.Errorf("Wrong namespace: %q instead of %q", action.GetNamespace(), expectedNamespace)
		}
		getAction := action.(testcore.GetAction)
		expectedName := "virtlet-foo42"
		if getAction.GetName() != expectedName {
			t.Errorf("Bad pod name: %q instead of %q", getAction.GetName(), expectedName)
		}
		return true, &v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "virtlet-foo42",
				Namespace: "kube-system",
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		}, nil
	})
	restClient := &fakerest.RESTClient{
		// NOTE: APIRegistry will no longer be necessary in newer client-go
		APIRegistry: api.Registry,
		NegotiatedSerializer://testapi.Default.NegotiatedSerializer(),
		dynamic.ContentConfig().NegotiatedSerializer,
		Client: fakerest.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			// this handler is not actually invoked
			return nil, nil
		}),
	}
	config := &rest.Config{Host: "foo-host"}
	pf := &fakePortForwarder{t: t}
	c := &RealKubeClient{
		client:        fc,
		config:        config,
		restClient:    restClient,
		portForwarder: pf,
	}
	portsToForward := []*ForwardedPort{
		{
			LocalPort:  59000,
			RemotePort: 5900,
		},
		{
			RemotePort: 5901,
		},
		{
			RemotePort: 5902,
		},
	}
	stopCh, err := c.ForwardPorts("virtlet-foo42", "kube-system", portsToForward)
	if err != nil {
		t.Fatalf("ForwardPorts(): %v", err)
	}
	if pf.config == nil {
		t.Errorf("pf.config not set")
	} else if pf.config.Host != "foo-host" {
		t.Errorf("Bad host in rest config: %q instead of foo-host", pf.config.Host)
	}
	if pf.method != "POST" {
		t.Errorf("Bad method %q instead of POST", pf.method)
	}

	expectedPath := "/namespaces/kube-system/pods/virtlet-foo42/portforward"
	if pf.url == nil {
		t.Errorf("pf.url is not set")
	} else if pf.url.Path != expectedPath {
		t.Errorf("Bad expectedPath: %q instead of %q", pf.url.Path, expectedPath)
	}

	expectedPortStr := "59000:5900 :5901 :5902"
	if pf.ports != expectedPortStr {
		t.Errorf("Bad requested port forward list: %q instead of %q", pf.ports, expectedPortStr)
	}

	expectedPorts := []*ForwardedPort{
		{
			LocalPort:  59000,
			RemotePort: 5900,
		},
		{
			LocalPort:  4243, // 1st "random" port
			RemotePort: 5901,
		},
		{
			LocalPort:  4244, // 2nd "random" port
			RemotePort: 5902,
		},
	}
	if !reflect.DeepEqual(expectedPorts, portsToForward) {
		t.Errorf("Bad ports:\n%s", spew.Sdump(portsToForward))
	}

	if stopCh == nil {
		t.Error("Stop channel is nil")
	} else {
		close(stopCh)
	}
}

// TODO: test not finding Virtlet pod
// TODO: add test for 'virsh' command
// TODO: don't require --node on a single-Virtlet-node clusters
