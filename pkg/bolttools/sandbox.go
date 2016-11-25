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
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/davecgh/go-spew/spew"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func (b *BoltClient) VerifySandboxSchema() error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte("sandbox")); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) SetPodSandbox(config *kubeapi.PodSandboxConfig, networkConfiguration []byte) error {
	podId := config.Metadata.GetUid()

	strLabels, err := json.Marshal(config.GetLabels())
	if err != nil {
		return err
	}

	strAnnotations, err := json.Marshal(config.GetAnnotations())
	if err != nil {
		return err
	}

	metadata := config.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("sandbox config is missing Metadata attribute: %s", spew.Sdump(config))
	}

	linuxSandbox := config.GetLinux()
	if linuxSandbox == nil {
		return fmt.Errorf("sandbox config is missing Linux attribute: %s", spew.Sdump(config))
	}

	namespaceOptions := linuxSandbox.GetNamespaceOptions()
	if namespaceOptions == nil {
		return fmt.Errorf("Linux sandbox config is missing Namespaces attribute: %s", spew.Sdump(config))
	}

	err = b.db.Batch(func(tx *bolt.Tx) error {
		parentBucket := tx.Bucket([]byte("sandbox"))
		if parentBucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		sandboxBucket, err := parentBucket.CreateBucketIfNotExists([]byte(podId))
		if err != nil {
			return err
		}

		if err := sandboxBucket.Put([]byte("networkConfiguration"), networkConfiguration); err != nil {
			return err
		}

		if err := sandboxBucket.Put([]byte("createdAt"), []byte(strconv.FormatInt(time.Now().Unix(), 10))); err != nil {
			return err
		}

		if err := sandboxBucket.Put([]byte("hostname"), []byte(config.GetHostname())); err != nil {
			return err
		}

		if err := sandboxBucket.Put([]byte("logDirectory"), []byte(config.GetLogDirectory())); err != nil {
			return err
		}

		if err := sandboxBucket.Put([]byte("labels"), []byte(strLabels)); err != nil {
			return err
		}

		if err := sandboxBucket.Put([]byte("annotations"), []byte(strAnnotations)); err != nil {
			return err
		}

		if err := sandboxBucket.Put([]byte("state"), []byte{byte(kubeapi.PodSandBoxState_READY)}); err != nil {
			return err
		}

		metadataBucket, err := sandboxBucket.CreateBucketIfNotExists([]byte("metadata"))
		if err != nil {
			return err
		}

		if err := metadataBucket.Put([]byte("name"), []byte(metadata.GetName())); err != nil {
			return err
		}

		if err := metadataBucket.Put([]byte("uid"), []byte(metadata.GetUid())); err != nil {
			return err
		}

		if err := metadataBucket.Put([]byte("namespace"), []byte(metadata.GetNamespace())); err != nil {
			return err
		}

		if err := metadataBucket.Put([]byte("attempt"), []byte(strconv.FormatUint(uint64(metadata.GetAttempt()), 10))); err != nil {
			return err
		}

		linuxSandboxBucket, err := sandboxBucket.CreateBucketIfNotExists([]byte("linuxSandbox"))
		if err != nil {
			return err
		}

		if err := linuxSandboxBucket.Put([]byte("cgroupParent"), []byte(linuxSandbox.GetCgroupParent())); err != nil {
			return err
		}

		namespaceOptionsBucket, err := linuxSandboxBucket.CreateBucketIfNotExists([]byte("namespaceOptions"))
		if err != nil {
			return err
		}

		if err := namespaceOptionsBucket.Put([]byte("hostNetwork"), []byte(strconv.FormatBool(namespaceOptions.GetHostNetwork()))); err != nil {
			return err
		}

		if err := namespaceOptionsBucket.Put([]byte("hostPid"), []byte(strconv.FormatBool(namespaceOptions.GetHostPid()))); err != nil {
			return err
		}

		if err := namespaceOptionsBucket.Put([]byte("hostIpc"), []byte(strconv.FormatBool(namespaceOptions.GetHostIpc()))); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) UpdatePodState(podId string, state kubeapi.PodSandBoxState) error {
	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sandbox"))
		if bucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		sandboxBucket := bucket.Bucket([]byte(podId))
		if sandboxBucket == nil {
			return fmt.Errorf("bucket '%s' doesn't exist", podId)
		}

		if err := sandboxBucket.Put([]byte("state"), []byte{byte(state)}); err != nil {
			return err
		}

		return nil
	})

	return err
}

func (b *BoltClient) RemovePodSandbox(podId string) error {
	if err := b.db.Batch(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sandbox"))
		if bucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		if err := bucket.DeleteBucket([]byte(podId)); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (b *BoltClient) GetPodSandboxStatus(podId string) (*kubeapi.PodSandboxStatus, error) {
	var podSandboxStatus *kubeapi.PodSandboxStatus

	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sandbox"))
		if bucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		sandboxBucket := bucket.Bucket([]byte(podId))
		if sandboxBucket == nil {
			return fmt.Errorf("bucket '%s' doesn't exist", podId)
		}

		byteCreatedAt, err := getString(sandboxBucket, "createdAt")
		if err != nil {
			return err
		}

		createdAt, err := strconv.ParseInt(byteCreatedAt, 10, 64)
		if err != nil {
			return err
		}

		byteLabels, err := get(sandboxBucket, []byte("labels"))
		if err != nil {
			return err
		}

		var labels map[string]string
		if err := json.Unmarshal(byteLabels, &labels); err != nil {
			return err
		}

		byteAnnotations, err := get(sandboxBucket, []byte("annotations"))
		if err != nil {
			return err
		}

		var annotations map[string]string
		if err := json.Unmarshal(byteAnnotations, &annotations); err != nil {
			return err
		}

		bytesState, err := get(sandboxBucket, []byte("state"))
		if err != nil {
			return err
		}

		metadataBucket := sandboxBucket.Bucket([]byte("metadata"))
		if metadataBucket == nil {
			return fmt.Errorf("bucket 'metadata' doesn't exist")
		}

		metadataName, err := getString(metadataBucket, "name")
		if err != nil {
			return err
		}

		metadataUid, err := getString(metadataBucket, "uid")
		if err != nil {
			return err
		}

		metadataNamespace, err := getString(metadataBucket, "namespace")
		if err != nil {
			return err
		}

		strMetadataAttempt, err := getString(metadataBucket, "attempt")
		if err != nil {
			return err
		}

		uintMetadataAttempt, err := strconv.ParseUint(strMetadataAttempt, 10, 32)
		if err != nil {
			return err
		}
		uint32MetadataAttempt := uint32(uintMetadataAttempt)

		linuxSandboxBucket := sandboxBucket.Bucket([]byte("linuxSandbox"))
		if linuxSandboxBucket == nil {
			return fmt.Errorf("bucket 'linuxSandbox' doesn't exist")
		}

		namespaceOptionsBucket := linuxSandboxBucket.Bucket([]byte("namespaceOptions"))
		if namespaceOptionsBucket == nil {
			return fmt.Errorf("bucket 'namespaceOptions' doesn't exist")
		}

		strHostNetwork, err := getString(namespaceOptionsBucket, "hostNetwork")
		if err != nil {
			return err
		}

		hostNetwork, err := strconv.ParseBool(strHostNetwork)
		if err != nil {
			return err
		}

		strHostPid, err := getString(namespaceOptionsBucket, "hostPid")
		if err != nil {
			return err
		}

		hostPid, err := strconv.ParseBool(strHostPid)
		if err != nil {
			return err
		}

		strHostIpc, err := getString(namespaceOptionsBucket, "hostIpc")
		if err != nil {
			return err
		}

		hostIpc, err := strconv.ParseBool(strHostIpc)
		if err != nil {
			return err
		}

		metadata := &kubeapi.PodSandboxMetadata{
			Name:      &metadataName,
			Uid:       &metadataUid,
			Namespace: &metadataNamespace,
			Attempt:   &uint32MetadataAttempt,
		}

		state := kubeapi.PodSandBoxState(bytesState[0])

		namespaceOptions := &kubeapi.NamespaceOption{
			HostNetwork: &hostNetwork,
			HostPid:     &hostPid,
			HostIpc:     &hostIpc,
		}

		namespace := &kubeapi.Namespace{
			Options: namespaceOptions,
		}

		linuxSandbox := &kubeapi.LinuxPodSandboxStatus{
			Namespaces: namespace,
		}

		podSandboxStatus = &kubeapi.PodSandboxStatus{
			Id:          &podId,
			Metadata:    metadata,
			State:       &state,
			CreatedAt:   &createdAt,
			Linux:       linuxSandbox,
			Labels:      labels,
			Annotations: annotations,
		}

		return nil
	})

	return podSandboxStatus, err
}

func filterPodSandbox(sandbox *kubeapi.PodSandbox, filter *kubeapi.PodSandboxFilter) bool {
	if filter.GetId() != "" && sandbox.GetId() != filter.GetId() {
		return false
	}

	if sandbox.GetState() != filter.GetState() {
		return false
	}

	if filter.GetLabelSelector() != nil {
		if sandbox.GetLabels() == nil {
			return false
		}

		for k, v := range filter.GetLabelSelector() {
			sv, ok := sandbox.GetLabels()[k]

			if !ok {
				return false
			}

			if v != sv {
				return false
			}
		}
	}

	return true
}

func (b *BoltClient) getPodSandbox(sandboxId []byte, filter *kubeapi.PodSandboxFilter) (*kubeapi.PodSandbox, bool, error) {
	var podSandbox *kubeapi.PodSandbox

	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sandbox"))
		if bucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		sandboxBucket := bucket.Bucket(sandboxId)
		if sandboxBucket == nil {
			return fmt.Errorf("bucket '%s' doesn't exist", sandboxId)
		}

		strCreatedAt, err := getString(sandboxBucket, "createdAt")
		if err != nil {
			return err
		}

		createdAt, err := strconv.ParseInt(strCreatedAt, 10, 64)
		if err != nil {
			return err
		}

		byteLabels, err := get(sandboxBucket, []byte("labels"))
		if err != nil {
			return err
		}

		var labels map[string]string
		if err := json.Unmarshal(byteLabels, &labels); err != nil {
			return err
		}

		bytesState, err := get(sandboxBucket, []byte("state"))
		if err != nil {
			return err
		}

		metadataBucket := sandboxBucket.Bucket([]byte("metadata"))
		if metadataBucket == nil {
			return fmt.Errorf("bucket 'metadata' doesn't exist")
		}

		metadataName, err := getString(metadataBucket, "name")
		if err != nil {
			return err
		}

		metadataUid, err := getString(metadataBucket, "uid")
		if err != nil {
			return err
		}

		metadataNamespace, err := getString(metadataBucket, "namespace")
		if err != nil {
			return err
		}

		metadataAttempt, err := getString(metadataBucket, "attempt")
		if err != nil {
			return err
		}

		uintMetadataAttempt, err := strconv.ParseUint(metadataAttempt, 10, 32)
		if err != nil {
			return err
		}
		uint32MetadataAttempt := uint32(uintMetadataAttempt)

		strSandboxId := string(sandboxId)

		metadata := &kubeapi.PodSandboxMetadata{
			Name:      &metadataName,
			Uid:       &metadataUid,
			Namespace: &metadataNamespace,
			Attempt:   &uint32MetadataAttempt,
		}

		state := kubeapi.PodSandBoxState(bytesState[0])

		podSandbox = &kubeapi.PodSandbox{
			Id:        &strSandboxId,
			Metadata:  metadata,
			State:     &state,
			CreatedAt: &createdAt,
			Labels:    labels,
		}

		return nil
	})
	if err != nil {
		return nil, false, err
	}

	match := filterPodSandbox(podSandbox, filter)

	return podSandbox, match, nil
}

func (b *BoltClient) ListPodSandbox(filter *kubeapi.PodSandboxFilter) ([]*kubeapi.PodSandbox, error) {
	sandboxIds := make([][]byte, 0)
	sandboxes := make([]*kubeapi.PodSandbox, 0)

	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sandbox"))
		if bucket == nil {
			// there is no sanbox bucket, so there are no pods
			return nil
		}

		bucket.ForEach(func(k, v []byte) error {
			sandboxIds = append(sandboxIds, k)

			return nil
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, sandboxId := range sandboxIds {
		sandbox, match, err := b.getPodSandbox(sandboxId, filter)
		if err != nil {
			return nil, err
		}

		if !match {
			continue
		}

		sandboxes = append(sandboxes, sandbox)
	}

	return sandboxes, nil
}

func (b *BoltClient) GetPodNetworkConfigurationAsBytes(podId string) ([]byte, error) {
	var config []byte
	err := b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sandbox"))
		if bucket == nil {
			return fmt.Errorf("bucket 'sandbox' doesn't exist")
		}

		sandboxBucket := bucket.Bucket([]byte(podId))
		if sandboxBucket == nil {
			return fmt.Errorf("bucket '%s' doesn't exist", podId)
		}

		config = sandboxBucket.Get([]byte("networkConfiguration"))

		return nil

	})
	return config, err
}
