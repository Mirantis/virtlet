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

package etcdtools

import (
	"fmt"
	"strconv"

	etcd "github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

type sandboxConverter struct {
	tool  *SandboxTool
	podId string
	// PodSandboxConfig
	hostnameKey     string
	logDirectoryKey string
	// PodSandboxConfig.Metadata
	metadataNameKey      string
	metadataUidKey       string
	metadataNamespaceKey string
	metadataAttemptKey   string
	// PodSandboxConfig.Linux
	linuxCgroupParent string
	// PodSandboxConfig.Linux.NamespaceOptions
	namespaceOptionsHostNetwork string
	namespaceOptionsHostPid     string
	namespaceOptionsHostIpc     string
}

func newSandboxConverter(podId string) *sandboxConverter {
	// PodSandboxConfig
	hostnameKey := fmt.Sprintf("/sandbox/%s/hostname", podId)
	logDirectoryKey := fmt.Sprintf("/sandbox/%s/logDirectory", podId)
	// PodSandboxConfig.Metadata
	metadataNameKey := fmt.Sprintf("/sandbox/%s/metadata/name", podId)
	metadataUidKey := fmt.Sprintf("/sandbox/%s/metadata/uid", podId)
	metadataNamespaceKey := fmt.Sprintf("/sandbox/%s/metadata/uid", podId)
	metadataAttemptKey := fmt.Sprintf("/sandbox/%s/metadata/attempt", podId)
	// PodSandboxConfig.Linux
	linuxCgroupParent := fmt.Sprintf("/sandbox/%s/linuxSandbox/cgroupParent", podId)
	// PodSandboxConfig.Linux.NamespaceOptions
	namespaceOptionsHostNetwork := fmt.Sprintf("/sandbox/%s/linuxSandbox/namespaceOptions/hostNetwork", podId)
	namespaceOptionsHostPid := fmt.Sprintf("/sandbox/%s/linuxSandbox/namespaceOptions/hostPid", podId)
	namespaceOptionsHostIpc := fmt.Sprintf("/sandbox/%s/linuxSandbox/namespaceOptions/hostIpc", podId)

	return &sandboxConverter{
		// PodSandboxConfig
		hostnameKey:     hostnameKey,
		logDirectoryKey: logDirectoryKey,
		// PodSandboxConfig.Metadata
		metadataNameKey:      metadataNameKey,
		metadataUidKey:       metadataUidKey,
		metadataNamespaceKey: metadataNamespaceKey,
		metadataAttemptKey:   metadataAttemptKey,
		// PodSandboxConfig.Linux
		linuxCgroupParent: linuxCgroupParent,
		// PodSandboxConfig.Linux.NamespaceOptions
		namespaceOptionsHostNetwork: namespaceOptionsHostNetwork,
		namespaceOptionsHostPid:     namespaceOptionsHostPid,
		namespaceOptionsHostIpc:     namespaceOptionsHostIpc,
	}
}

func (c *sandboxConverter) sandboxConfigToEtcd(config *kubeapi.PodSandboxConfig) error {
	// PodSandboxConfig
	_, err := c.tool.kapi.Set(context.Background(), c.hostnameKey, *config.Hostname, nil)
	if err != nil {
		return err
	}

	_, err = c.tool.kapi.Set(context.Background(), c.logDirectoryKey, *config.LogDirectory, nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Metadata
	_, err = c.tool.kapi.Set(context.Background(), c.metadataNameKey, *config.Metadata.Name, nil)
	if err != nil {
		return err
	}

	_, err = c.tool.kapi.Set(context.Background(), c.metadataUidKey, *config.Metadata.Uid, nil)
	if err != nil {
		return err
	}

	_, err = c.tool.kapi.Set(context.Background(), c.metadataNamespaceKey, *config.Metadata.Namespace, nil)
	if err != nil {
		return err
	}

	_, err = c.tool.kapi.Set(context.Background(), c.metadataAttemptKey, string(*config.Metadata.Attempt), nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Linux
	_, err = c.tool.kapi.Set(context.Background(), c.linuxCgroupParent, *config.Linux.CgroupParent, nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Linux.NamespaceOptions
	_, err = c.tool.kapi.Set(context.Background(), c.namespaceOptionsHostNetwork, strconv.FormatBool(*config.Linux.NamespaceOptions.HostNetwork), nil)
	if err != nil {
		return err
	}

	_, err = c.tool.kapi.Set(context.Background(), c.namespaceOptionsHostPid, strconv.FormatBool(*config.Linux.NamespaceOptions.HostPid), nil)
	if err != nil {
		return err
	}

	_, err = c.tool.kapi.Set(context.Background(), c.namespaceOptionsHostIpc, strconv.FormatBool(*config.Linux.NamespaceOptions.HostIpc), nil)
	if err != nil {
		return err
	}

	return nil
}

func (c *sandboxConverter) etcdToSandboxMetadata() (*kubeapi.PodSandboxMetadata, error) {
	resp, err := c.tool.kapi.Get(context.Background(), c.metadataNameKey, nil)
	if err != nil {
		return nil, err
	}
	metadataName := resp.Node.Value

	resp, err = c.tool.kapi.Get(context.Background(), c.metadataUidKey, nil)
	if err != nil {
		return nil, err
	}
	metadataUid := resp.Node.Value

	resp, err = c.tool.kapi.Get(context.Background(), c.metadataNamespaceKey, nil)
	if err != nil {
		return nil, err
	}
	metadataNamespace := resp.Node.Value

	resp, err = c.tool.kapi.Get(context.Background(), c.metadataAttemptKey, nil)
	if err != nil {
		return nil, err
	}
	metadataAttempt, err := strconv.ParseUint(resp.Node.Value, 10, 32)
	if err != nil {
		return nil, err
	}
	metadataAttempt32 := uint32(metadataAttempt)

	return &kubeapi.PodSandboxMetadata{
		Name:      &metadataName,
		Uid:       &metadataUid,
		Namespace: &metadataNamespace,
		Attempt:   &metadataAttempt32,
	}, nil
}

func (c *sandboxConverter) etcdToSandboxStatus() (*kubeapi.PodSandboxStatus, error) {
	// PodSandboxStatus.Linux.Namespace.Options
	resp, err := c.tool.kapi.Get(context.Background(), c.namespaceOptionsHostNetwork, nil)
	if err != nil {
		return nil, err
	}
	namespaceOptionsHostNetwork, err := strconv.ParseBool(resp.Node.Value)
	if err != nil {
		return nil, err
	}

	resp, err = c.tool.kapi.Get(context.Background(), c.namespaceOptionsHostPid, nil)
	if err != nil {
		return nil, err
	}
	namespaceOptionsHostPid, err := strconv.ParseBool(resp.Node.Value)
	if err != nil {
		return nil, err
	}

	resp, err = c.tool.kapi.Get(context.Background(), c.namespaceOptionsHostIpc, nil)
	if err != nil {
		return nil, err
	}
	namespaceOptionsHostIpc, err := strconv.ParseBool(resp.Node.Value)
	if err != nil {
		return nil, err
	}

	namespaceOptions := &kubeapi.NamespaceOption{
		HostNetwork: &namespaceOptionsHostNetwork,
		HostPid:     &namespaceOptionsHostPid,
		HostIpc:     &namespaceOptionsHostIpc,
	}

	// PodSandboxStatus.Linux.Namespace

	network := ""
	namespace := &kubeapi.Namespace{
		Network: &network,
		Options: namespaceOptions,
	}

	// PodSandboxStatus.Linux
	linuxPodSandboxStatus := &kubeapi.LinuxPodSandboxStatus{
		Namespaces: namespace,
	}

	// PodSandboxStatus.Network
	ip := "10.0.0.2"
	podSandboxNetworkStatus := &kubeapi.PodSandboxNetworkStatus{
		Ip: &ip,
	}

	// PodSandboxStatus.Metadata
	podSandboxMetadata, err := c.etcdToSandboxMetadata()
	if err != nil {
		return nil, err
	}

	// PodSandboxConfig
	state := kubeapi.PodSandBoxState_READY
	createdAt := int64(0)
	podSandboxStatus := &kubeapi.PodSandboxStatus{
		Id:          &c.podId,
		Metadata:    podSandboxMetadata,
		State:       &state,
		CreatedAt:   &createdAt,
		Network:     podSandboxNetworkStatus,
		Linux:       linuxPodSandboxStatus,
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}

	return podSandboxStatus, nil
}

func (c *sandboxConverter) etcdToSandbox() (*kubeapi.PodSandbox, error) {
	podSandboxMetadata, err := c.etcdToSandboxMetadata()
	if err != nil {
		return nil, err
	}
	state := kubeapi.PodSandBoxState_READY
	createdAt := int64(0)

	return &kubeapi.PodSandbox{
		Id:        &c.podId,
		Metadata:  podSandboxMetadata,
		State:     &state,
		CreatedAt: &createdAt,
		Labels:    make(map[string]string),
	}, nil
}

type SandboxTool struct {
	kapi etcd.KeysAPI
}

func NewSandboxTool(kapi etcd.KeysAPI) *SandboxTool {
	return &SandboxTool{kapi: kapi}
}

func (s *SandboxTool) CreatePodSandbox(podId string, config *kubeapi.PodSandboxConfig) error {
	c := newSandboxConverter(podId)
	if err := c.sandboxConfigToEtcd(config); err != nil {
		return err
	}
	return nil
}

func (s *SandboxTool) PodSandboxStatus(podId string) (*kubeapi.PodSandboxStatus, error) {
	c := newSandboxConverter(podId)
	status, err := c.etcdToSandboxStatus()
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (s *SandboxTool) ListPodSandbox() ([]*kubeapi.PodSandbox, error) {
	resp, err := s.kapi.Get(context.Background(), "/sandbox", nil)
	if err != nil {
		return []*kubeapi.PodSandbox{}, nil
	}

	podSandboxList := make([]*kubeapi.PodSandbox, 0, resp.Node.Nodes.Len())
	for _, node := range resp.Node.Nodes {
		podId := node.Key
		c := newSandboxConverter(podId)
		podSandbox, err := c.etcdToSandbox()
		if err != nil {
			return nil, err
		}
		podSandboxList = append(podSandboxList, podSandbox)
	}

	return podSandboxList, nil
}
