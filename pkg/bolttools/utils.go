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

package bolttools

import (
	"encoding/json"
	"fmt"
	virtletutils "github.com/Mirantis/virtlet/pkg/utils"
	"github.com/boltdb/bolt"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
	"strconv"
	"testing"
)

type ContainerTestConfigSet struct {
	SandboxId   string
	ContainerId string
	Image       string
	Labels      map[string]string
	Annotations map[string]string
}

func get(bucket *bolt.Bucket, key []byte) ([]byte, error) {
	value := bucket.Get(key)
	if value == nil {
		return nil, fmt.Errorf("key '%s' doesn't exist in the bucket", key)
	}

	return value, nil
}

func getString(bucket *bolt.Bucket, key string) (string, error) {
	value, err := get(bucket, []byte(key))
	if err != nil {
		return "", err
	}

	return string(value), nil
}

func RemoveElFromJsonArray(jsonArr []byte, toDelete string) ([]byte, error) {
	var ind uint32 = 0

	arr := make([]string, 0)
	err := json.Unmarshal(jsonArr, &arr)
	if err != nil {
		return nil, err
	}

	if len(arr) == 0 {
		return jsonArr, nil
	}

	for _, el := range arr {
		if el == toDelete {
			break
		}
		ind++
	}
	arr = append(arr[:ind], arr[(ind+1):]...)
	newJsonArr, err := json.Marshal(arr)
	if err != nil {
		return nil, err
	}
	return newJsonArr, nil
}

func AddElToJsonArray(jsonArr []byte, toAdd string) ([]byte, error) {
	arr := make([]string, 0)
	err := json.Unmarshal(jsonArr, &arr)
	if err != nil {
		return nil, err
	}
	arr = append(arr, toAdd)
	newJsonArr, err := json.Marshal(arr)
	if err != nil {
		return nil, err
	}
	return newJsonArr, nil
}

func dumpDB(t *testing.T, db *bolt.DB) error {
	t.Log("=======Start DB content")
	err := db.View(func(tx *bolt.Tx) error {
		var iterateOverElements func(tx *bolt.Tx, bucket *bolt.Bucket, indent string)
		iterateOverElements = func(tx *bolt.Tx, bucket *bolt.Bucket, indent string) {
			var c *bolt.Cursor
			if bucket == nil {
				c = tx.Cursor()
			} else {
				c = bucket.Cursor()
			}
			for k, v := c.First(); k != nil; k, v = c.Next() {
				if v == nil {
					// SubBucket
					t.Logf(" %s BUCKET: %s", indent, string(k))
					if bucket == nil {
						//root bucket
						iterateOverElements(tx, tx.Bucket(k), "  "+indent)
					} else {
						iterateOverElements(tx, bucket.Bucket(k), "  "+indent)
					}
				} else {
					t.Logf(" %s %s: %s\n", indent, string(k), string(v))
				}
			}
		}
		iterateOverElements(tx, nil, "|_")
		return nil
	})
	t.Log("=======End DB content")

	return err
}

func GetSandboxes(sandboxNum int) ([]*kubeapi.PodSandboxConfig, error) {
	sandboxes := []*kubeapi.PodSandboxConfig{}

	for i := 0; i < sandboxNum; i++ {
		name := "testName_" + strconv.Itoa(i)
		uid, err := virtletutils.NewUuid()

		if err != nil {
			return nil, err
		}

		namespace := "default"
		attempt := uint32(0)
		metadata := &kubeapi.PodSandboxMetadata{
			Name:      &name,
			Uid:       &uid,
			Namespace: &namespace,
			Attempt:   &attempt,
		}

		hostNetwork := false
		hostPid := false
		hostIpc := false
		namespaceOptions := &kubeapi.NamespaceOption{
			HostNetwork: &hostNetwork,
			HostPid:     &hostPid,
			HostIpc:     &hostIpc,
		}

		cgroupParent := ""
		linuxSandbox := &kubeapi.LinuxPodSandboxConfig{
			CgroupParent:     &cgroupParent,
			NamespaceOptions: namespaceOptions,
		}

		hostname := "localhost"
		logDirectory := "/var/log/test_log_directory"
		sandboxConfig := &kubeapi.PodSandboxConfig{
			Metadata:     metadata,
			Hostname:     &hostname,
			LogDirectory: &logDirectory,
			Labels: map[string]string{
				"foo":  "bar",
				"fizz": "buzz",
			},
			Annotations: map[string]string{
				"hello": "world",
				"virt":  "let",
			},
			Linux: linuxSandbox,
		}

		sandboxes = append(sandboxes, sandboxConfig)
	}

	return sandboxes, nil
}

func GetContainersConfig(sandboxConfigs []*kubeapi.PodSandboxConfig) ([]*ContainerTestConfigSet, error) {
	containers := []*ContainerTestConfigSet{}

	for _, sandbox := range sandboxConfigs {
		uid, err := virtletutils.NewUuid()

		if err != nil {
			return nil, err
		}
		containerConf := &ContainerTestConfigSet{
			SandboxId:   *sandbox.Metadata.Uid,
			Image:       "testImage",
			ContainerId: uid,
			Labels:      map[string]string{"foo": "bar", "fizz": "buzz"},
			Annotations: map[string]string{"hello": "world", "virt": "let"},
		}
		containers = append(containers, containerConf)
	}

	return containers, nil
}

func SetUpBolt(t *testing.T, sandboxConfigs []*kubeapi.PodSandboxConfig, containerConfigs []*ContainerTestConfigSet) *BoltClient {
	b, err := NewFakeBoltClient()
	if err != nil {
		t.Fatal(err)
	}
	//Check filter on empty DB
	sandboxList, err := b.ListPodSandbox(&kubeapi.PodSandboxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if sandboxList == nil || len(sandboxList) != 0 {
		t.Errorf("Expected to recieve array of zero lenght as a result of list request against empty Bolt db.")
	}

	if err := b.VerifySandboxSchema(); err != nil {
		t.Fatal(err)
	}

	for _, sandbox := range sandboxConfigs {
		if err := b.SetPodSandbox(sandbox); err != nil {
			t.Fatal(err)
		}
	}

	if err := b.VerifyVirtualizationSchema(); err != nil {
		t.Fatal(err)
	}

	for _, container := range containerConfigs {
		if err := b.SetContainer(container.ContainerId, container.SandboxId, container.Image, container.Labels, container.Annotations); err != nil {
			t.Fatal(err)
		}
	}

	dumpDB(t, b.db)

	return b
}
