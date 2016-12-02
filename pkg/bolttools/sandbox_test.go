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
	"fmt"
	"reflect"
	"testing"

	"github.com/boltdb/bolt"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func TestSetPodSandbox(t *testing.T) {
	name := "testName"
	uid := "f1836e9d-b386-432f-9d41-f34d8e5d6a59"
	namespace := "testNamespace"
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

	cgroupParent := "testCgroupParent"
	linuxSandbox := &kubeapi.LinuxPodSandboxConfig{
		CgroupParent:     &cgroupParent,
		NamespaceOptions: namespaceOptions,
	}

	hostname := "testHostname"
	logDirectory := "/var/log/test_log_directory"
	validSandboxConfig := &kubeapi.PodSandboxConfig{
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

	tests := []struct {
		config              *kubeapi.PodSandboxConfig
		expectedLabels      string
		expectedAnnotations string
		error               bool
	}{
		{
			config:              validSandboxConfig,
			expectedLabels:      "{\"fizz\":\"buzz\",\"foo\":\"bar\"}",
			expectedAnnotations: "{\"hello\":\"world\",\"virt\":\"let\"}",
			error:               false,
		},
		{
			config: &kubeapi.PodSandboxConfig{},
			error:  true,
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.VerifySandboxSchema(); err != nil {
			t.Fatal(err)
		}

		if err := b.SetPodSandbox(tc.config, []byte{}); err != nil {
			if tc.error {
				continue
			}

			t.Fatal(err)
		}

		if err := b.db.View(func(tx *bolt.Tx) error {
			parentBucket := tx.Bucket([]byte("sandbox"))
			if parentBucket == nil {
				return fmt.Errorf("bucket 'sandbox' doesn't exist")
			}

			bucket := parentBucket.Bucket([]byte(tc.config.GetMetadata().GetUid()))
			if bucket == nil {
				return fmt.Errorf("bucket '%s' doesn't exist", tc.config.GetMetadata().GetUid())
			}

			hostname, err := getString(bucket, "hostname")
			if err != nil {
				return err
			}
			if hostname != tc.config.GetHostname() {
				t.Errorf("Expected %s, instead got %s", tc.config.GetHostname(), hostname)
			}

			strLabels, err := getString(bucket, "labels")
			if err != nil {
				return err
			}
			if strLabels != tc.expectedLabels {
				t.Errorf("Expected %s, instead got %s", tc.expectedLabels, strLabels)
			}

			strAnnotations, err := getString(bucket, "annotations")
			if err != nil {
				return err
			}
			if strAnnotations != tc.expectedAnnotations {
				t.Errorf("Expected %s, instead got %s", tc.expectedAnnotations, strAnnotations)
			}

			metadataBucket := bucket.Bucket([]byte("metadata"))
			if metadataBucket == nil {
				return fmt.Errorf("bucket 'metadata' doesn't exist")
			}

			name, err := getString(metadataBucket, "name")
			if err != nil {
				return err
			}
			if name != tc.config.GetMetadata().GetName() {
				t.Errorf("Expected %s, instead got %s", tc.config.GetMetadata().GetName(), name)
			}

			uid, err := getString(metadataBucket, "uid")
			if err != nil {
				return err
			}
			if uid != tc.config.GetMetadata().GetUid() {
				t.Errorf("Expected %s, instead got %s", tc.config.GetMetadata().GetUid(), uid)
			}

			namespace, err := getString(metadataBucket, "namespace")
			if err != nil {
				return err
			}
			if namespace != tc.config.GetMetadata().GetNamespace() {
				t.Errorf("Expected %s, instead got %s", tc.config.GetMetadata().GetNamespace(), namespace)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRemovePodSandbox(t *testing.T) {
	name := "testName"
	uid := "f1836e9d-b386-432f-9d41-f34d8e5d6a59"
	namespace := "testNamespace"
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

	cgroupParent := "testCgroupParent"
	linuxSandbox := &kubeapi.LinuxPodSandboxConfig{
		CgroupParent:     &cgroupParent,
		NamespaceOptions: namespaceOptions,
	}

	hostname := "testHostname"
	logDirectory := "/var/log/test_log_directory"
	sandbox := &kubeapi.PodSandboxConfig{
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

	tests := []struct {
		sandbox *kubeapi.PodSandboxConfig
		error   bool
	}{
		{
			sandbox: sandbox,
			error:   false,
		},
		{
			sandbox: nil,
			error:   true,
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.VerifySandboxSchema(); err != nil {
			t.Fatal(err)
		}

		if tc.sandbox != nil {
			if err := b.SetPodSandbox(tc.sandbox, []byte{}); err != nil {
				t.Fatal(err)
			}
		}

		if err := b.RemovePodSandbox(tc.sandbox.GetMetadata().GetUid()); err != nil {
			if tc.error {
				continue
			}

			t.Fatal(err)
		}
	}
}

func TestGetPodSandboxStatus(t *testing.T) {
	name := "testName"
	uid := "f1836e9d-b386-432f-9d41-f34d8e5d6a59"
	namespace := "testNamespace"
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

	cgroupParent := "testCgroupParent"
	linuxSandbox := &kubeapi.LinuxPodSandboxConfig{
		CgroupParent:     &cgroupParent,
		NamespaceOptions: namespaceOptions,
	}

	hostname := "testHostname"
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

	tests := []struct {
		config *kubeapi.PodSandboxConfig
	}{
		{
			config: sandboxConfig,
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.VerifySandboxSchema(); err != nil {
			t.Fatal(err)
		}

		if err := b.SetPodSandbox(tc.config, []byte{}); err != nil {
			t.Fatal(err)
		}

		status, err := b.GetPodSandboxStatus(tc.config.GetMetadata().GetUid())
		if err != nil {
			t.Fatal(err)
		}

		if status.GetState() != kubeapi.PodSandBoxState_READY {
			t.Errorf("Sandbox state not ready")
		}

		if !reflect.DeepEqual(status.GetLabels(), tc.config.GetLabels()) {
			t.Errorf("Expected %v, instead got %v", tc.config.GetLabels(), status.GetLabels())
		}

		if !reflect.DeepEqual(status.GetAnnotations(), tc.config.GetAnnotations()) {
			t.Errorf("Expected %v, instead got %v", tc.config.GetAnnotations(), status.GetAnnotations())
		}

		if status.GetMetadata().GetName() != tc.config.GetMetadata().GetName() {
			t.Errorf("Expected %s, instead got %s", tc.config.GetMetadata().GetName(), status.GetMetadata().GetName())
		}
	}
}

func TestListPodSandbox(t *testing.T) {
	firstName := "testName"
	firstUid := "f1836e9d-b386-432f-9d41-f34d8e5d6a59"
	firstNamespace := "testNamespace"
	firstAttempt := uint32(0)
	firstMetadata := &kubeapi.PodSandboxMetadata{
		Name:      &firstName,
		Uid:       &firstUid,
		Namespace: &firstNamespace,
		Attempt:   &firstAttempt,
	}

	firstHostNetwork := false
	firstHostPid := false
	firstHostIpc := false
	firstNamespaceOptions := &kubeapi.NamespaceOption{
		HostNetwork: &firstHostNetwork,
		HostPid:     &firstHostPid,
		HostIpc:     &firstHostIpc,
	}

	firstCgroupParent := "testCgroupParent"
	firstLinuxSandbox := &kubeapi.LinuxPodSandboxConfig{
		CgroupParent:     &firstCgroupParent,
		NamespaceOptions: firstNamespaceOptions,
	}

	firstHostname := "testHostname"
	firstLogDirectory := "/var/log/test_log_directory"
	firstSandboxConfig := &kubeapi.PodSandboxConfig{
		Metadata:     firstMetadata,
		Hostname:     &firstHostname,
		LogDirectory: &firstLogDirectory,
		Labels: map[string]string{
			"foo": "bar",
		},
		Annotations: map[string]string{
			"hello": "world",
			"virt":  "let",
		},
		Linux: firstLinuxSandbox,
	}

	secondName := "anotherTestName"
	secondUid := "cefda818-cc0b-4ff5-b6f9-759d3da96d63"
	secondNamespace := "testNamespace"
	secondAttempt := uint32(0)
	secondMetadata := &kubeapi.PodSandboxMetadata{
		Name:      &secondName,
		Uid:       &secondUid,
		Namespace: &secondNamespace,
		Attempt:   &secondAttempt,
	}

	secondHostNetwork := false
	secondHostPid := false
	secondHostIpc := false
	secondNamespaceOptions := &kubeapi.NamespaceOption{
		HostNetwork: &secondHostNetwork,
		HostPid:     &secondHostPid,
		HostIpc:     &secondHostIpc,
	}

	secondCgroupParent := "testCgroupParent"
	secondLinuxSandbox := &kubeapi.LinuxPodSandboxConfig{
		CgroupParent:     &secondCgroupParent,
		NamespaceOptions: secondNamespaceOptions,
	}

	secondHostname := "testHostname"
	secondLogDirectory := "/var/log/test_log_directory"
	secondSandboxConfig := &kubeapi.PodSandboxConfig{
		Metadata:     secondMetadata,
		Hostname:     &secondHostname,
		LogDirectory: &secondLogDirectory,
		Labels: map[string]string{
			"fizz": "buzz",
		},
		Annotations: map[string]string{
			"hello": "world",
			"virt":  "let",
		},
		Linux: secondLinuxSandbox,
	}

	state_ready := kubeapi.PodSandBoxState_READY
	state_notready := kubeapi.PodSandBoxState_NOTREADY

	tests := []struct {
		configs       []*kubeapi.PodSandboxConfig
		filter        *kubeapi.PodSandboxFilter
		expectedCount int
		error         bool
	}{
		{
			configs: []*kubeapi.PodSandboxConfig{
				firstSandboxConfig,
				secondSandboxConfig,
			},
			filter:        &kubeapi.PodSandboxFilter{},
			expectedCount: 2,
			error:         false,
		},
		{
			configs: []*kubeapi.PodSandboxConfig{
				firstSandboxConfig,
				secondSandboxConfig,
			},
			filter: &kubeapi.PodSandboxFilter{
				Id: &firstUid,
			},
			expectedCount: 1,
			error:         false,
		},
		{
			configs: []*kubeapi.PodSandboxConfig{
				firstSandboxConfig,
				secondSandboxConfig,
			},
			filter: &kubeapi.PodSandboxFilter{
				State: &state_ready,
			},
			expectedCount: 2,
			error:         false,
		},
		{
			configs: []*kubeapi.PodSandboxConfig{
				firstSandboxConfig,
				secondSandboxConfig,
			},
			filter: &kubeapi.PodSandboxFilter{
				State: &state_notready,
			},
			expectedCount: 0,
			error:         false,
		},
		{
			configs: []*kubeapi.PodSandboxConfig{
				firstSandboxConfig,
				secondSandboxConfig,
			},
			filter: &kubeapi.PodSandboxFilter{
				LabelSelector: map[string]string{
					"foo": "bar",
				},
			},
			expectedCount: 1,
			error:         false,
		},
		{
			configs:       []*kubeapi.PodSandboxConfig{},
			filter:        &kubeapi.PodSandboxFilter{},
			expectedCount: 0,
			error:         true,
		},
	}

	for _, tc := range tests {
		b, err := NewFakeBoltClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := b.VerifySandboxSchema(); err != nil {
			t.Fatal(err)
		}

		for _, config := range tc.configs {
			if err := b.SetPodSandbox(config, []byte{}); err != nil {
				t.Fatal(err)
			}
		}

		sandboxes, err := b.ListPodSandbox(tc.filter)
		if err != nil {
			if tc.error {
				continue
			}
			t.Fatal(err)
		}

		if len(sandboxes) != tc.expectedCount {
			t.Errorf("Expected %d sandboxes, instead got %d", tc.expectedCount, len(sandboxes))
		}
	}
}
