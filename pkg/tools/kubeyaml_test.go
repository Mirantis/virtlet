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
	"reflect"
	"testing"

	"github.com/Mirantis/virtlet/tests/gm"
	"github.com/davecgh/go-spew/spew"
	v1 "k8s.io/api/core/v1"
)

func TestLoadStoreYaml(t *testing.T) {
	tstYaml := `---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: virtlet
  namespace: kube-system
---
apiVersion: v1
kind: Pod
metadata:
  name: test
spec:
  containers:
  - name: tst
    image: docker.io/busybox`
	objs, err := LoadYaml([]byte(tstYaml))
	if err != nil {
		t.Fatalf("LoadYaml: %v", err)
	}
	if len(objs) != 2 {
		t.Errorf("expected 2 objects but got %d", len(objs))
	} else {
		if _, ok := objs[0].(*v1.ServiceAccount); !ok {
			t.Errorf("expected *v1.ServiceAccount as the 1st object but got:\n%s", spew.Sdump(objs[0]))
		}
		if _, ok := objs[1].(*v1.Pod); !ok {
			t.Errorf("expected *v1.Pod as the 2nd object but got:\n%s", spew.Sdump(objs[1]))
		}
	}
	out, err := ToYaml(objs)
	if err != nil {
		t.Fatalf("ToYaml: %v", err)
	}
	reloaded, err := LoadYaml(out)
	if err != nil {
		t.Fatalf("reloading yaml: %v", err)
	}
	if !reflect.DeepEqual(objs, reloaded) {
		t.Errorf("object changed after reloading. Was:\n%s\nReloaded:\n%s", spew.Sdump(objs), spew.Sdump(reloaded))
	}
	gm.Verify(t, gm.NewYamlVerifier(out))
}
