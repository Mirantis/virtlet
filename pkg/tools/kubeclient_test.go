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
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

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
	sampleContainerId = "docker://virtlet.cloud__2232e3bf-d702-5824-5e3c-f12e60e616b0"
)

type fakeExecutor struct {
	t             *testing.T
	config        *rest.Config
	method        string
	url           *url.URL
	streamOptions *remotecommand.StreamOptions
}

func fakeExecutorFactory(t *testing.T, dest *fakeExecutor) ExecutorFactory {
	return func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
		*dest = fakeExecutor{
			t:      t,
			config: config,
			method: method,
			url:    url,
		}
		return dest, nil
	}
}

func (e *fakeExecutor) Stream(options remotecommand.StreamOptions) error {
	if e.streamOptions != nil {
		e.t.Errorf("Stream called twice")
	}
	e.streamOptions = &options
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
						Name:      "virtlet-g9wtz",
						Namespace: "kube-system",
						Labels: map[string]string{
							"runtime": "virtlet",
						},
					},
				},
				{
					ObjectMeta: meta_v1.ObjectMeta{
						Name:      "virtlet-foo42",
						Namespace: "kube-system",
						Labels: map[string]string{
							"runtime": "virtlet",
						},
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
	podNames, err := c.GetVirtletPodNames()
	if err != nil {
		t.Fatalf("GetVirtletPodNames(): %v", err)
	}
	podNamesStr := strings.Join(podNames, ",")
	expectedPodNamesStr := "virtlet-g9wtz,virtlet-foo42"
	if podNamesStr != expectedPodNamesStr {
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
						ContainerID: sampleContainerId,
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
		ContainerId:    sampleContainerId,
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
	var fe fakeExecutor
	c := &RealKubeClient{
		client:          &fakekube.Clientset{},
		config:          config,
		restClient:      restClient,
		executorFactory: fakeExecutorFactory(t, &fe),
	}
	var stdin, stdout, stderr bytes.Buffer
	exitCode, err := c.ExecInContainer("virtlet-foo42", "virtlet", "kube-system", &stdin, &stdout, &stderr, []string{"echo", "foobar"})
	if err != nil {
		t.Errorf("ExecInContainer returned error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("ExecInContainer returned non-zero exit code %v", exitCode)
	}
	if fe.config.Host != "foo-host" {
		t.Errorf("Bad host in rest config: %q instead of foo-host", fe.config.Host)
	}
	if fe.method != "POST" {
		t.Errorf("Bad method %q instead of POST", fe.method)
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

	expectedPath := "/namespaces/kube-system/pods/virtlet-foo42/exec"
	if fe.url.Path != expectedPath {
		t.Errorf("Bad expectedPath: %q instead of %q", fe.url.Path, expectedPath)
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

// TODO: test not finding Virtlet pod
// TODO: add checks for whether the target pod is a VM pod
// (via the pod annotation, the runtime name must be configurable though)
// TODO: add test for 'virsh' command
// TODO: don't require --node on a single-Virtlet-node clusters
