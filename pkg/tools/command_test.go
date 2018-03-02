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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		listAction := action.(testcore.ListAction)
		expectedLabels := "runtime=virtlet"
		lr := listAction.GetListRestrictions()
		if lr.Labels.String() != expectedLabels {
			t.Errorf("bad labels: %q instead of %q", lr.Labels, expectedLabels)
		}
		if !lr.Fields.Empty() {
			t.Errorf("bad field selectors: %q instead of empty", lr.Fields)
		}

		return true, &v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "virtlet-g9wtz",
						Namespace: "kube-system",
						Labels: map[string]string{
							"runtime": "virtlet",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "virtlet-foo42",
						Namespace: "kube-system",
						Labels: map[string]string{
							"runtime": "virtlet",
						},
					},
				},
			},
		}, nil
	})

	c := &VirtletCommand{client: fc}
	podNames, err := c.GetVirtletPodNames()
	if err != nil {
		t.Fatalf("GetVirtletPodNames(): %v", err)
	}
	podNamesStr := strings.Join(podNames, ",")
	expectedPodNamesStr := "virtlet-g9wtz,virtlet-foo42"
	if podNamesStr != expectedPodNamesStr {
		t.Errorf("bad pod names: %q instead of %q", podNamesStr, expectedPodNamesStr)
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
	c := &VirtletCommand{
		client:          &fakekube.Clientset{},
		config:          config,
		restClient:      restClient,
		executorFactory: fakeExecutorFactory(t, &fe),
	}
	var stdin, stdout, stderr bytes.Buffer
	exitCode, err := c.ExecInContainer("virtlet-foo42", "virtlet", "kube-system", &stdin, &stdout, &stderr, "echo", "foobar")
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
