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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	// PodSandboxConfig.Labels
	labelsKey string
	// PodSandboxConfig.Annotations
	annotationsKey string
}

func newSandboxConverter(tool *SandboxTool, podId string) *sandboxConverter {
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
	// PodSandboxConfig.Labels
	labelsKey := fmt.Sprintf("/sandbox/%s/labels", podId)
	// PodSandboxConfig.Annotations
	annotationsKey := fmt.Sprintf("/sandbox/%s/annotations", podId)

	return &sandboxConverter{
		tool:  tool,
		podId: podId,
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
		// PodSandboxConfig.Labels
		labelsKey: labelsKey,
		// PodSandboxConfig.Annotations
		annotationsKey: annotationsKey,
	}
}

func (c *sandboxConverter) sandboxConfigToEtcd(config *kubeapi.PodSandboxConfig) error {
	kapi, err := c.tool.keysAPITool.newKeysAPI()
	if err != nil {
		return err
	}

	// PodSandboxConfig
	var hostname string
	if config.Hostname != nil {
		hostname = *config.Hostname
	}
	fmt.Printf("%#v %#v\n", c.tool)
	_, err = kapi.Set(context.Background(), c.hostnameKey, hostname, nil)
	if err != nil {
		return err
	}

	var logDirectory string
	if config.LogDirectory != nil {
		logDirectory = *config.LogDirectory
	}
	_, err = kapi.Set(context.Background(), c.logDirectoryKey, logDirectory, nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Metadata
	var metadataName string
	if config.Metadata.Name != nil {
		metadataName = *config.Metadata.Name
	}
	_, err = kapi.Set(context.Background(), c.metadataNameKey, metadataName, nil)
	if err != nil {
		return err
	}

	var metadataUid string
	if config.Metadata.Uid != nil {
		metadataUid = *config.Metadata.Uid
	}
	_, err = kapi.Set(context.Background(), c.metadataUidKey, metadataUid, nil)
	if err != nil {
		return err
	}

	var metadataNamespace string
	if config.Metadata.Namespace != nil {
		metadataNamespace = *config.Metadata.Namespace
	}
	_, err = kapi.Set(context.Background(), c.metadataNamespaceKey, metadataNamespace, nil)
	if err != nil {
		return err
	}

	var metadataAttempt string
	if config.Metadata.Attempt != nil {
		metadataAttempt = strconv.FormatUint(uint64(*config.Metadata.Attempt), 32)
	}
	_, err = kapi.Set(context.Background(), c.metadataAttemptKey, metadataAttempt, nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Linux
	var cgroupParent string
	if config.Linux.CgroupParent != nil {
		cgroupParent = *config.Linux.CgroupParent
	}
	_, err = kapi.Set(context.Background(), c.linuxCgroupParent, cgroupParent, nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Linux.NamespaceOptions
	var hostNetwork string
	if config.Linux.NamespaceOptions.HostNetwork != nil {
		hostNetwork = strconv.FormatBool(*config.Linux.NamespaceOptions.HostNetwork)
	}
	_, err = kapi.Set(context.Background(), c.namespaceOptionsHostNetwork, hostNetwork, nil)
	if err != nil {
		return err
	}

	var hostPid string
	if config.Linux.NamespaceOptions.HostPid != nil {
		hostPid = strconv.FormatBool(*config.Linux.NamespaceOptions.HostPid)
	}
	_, err = kapi.Set(context.Background(), c.namespaceOptionsHostPid, hostPid, nil)
	if err != nil {
		return err
	}

	var hostIpc string
	if config.Linux.NamespaceOptions.HostIpc != nil {
		hostIpc = strconv.FormatBool(*config.Linux.NamespaceOptions.HostIpc)
	}
	_, err = kapi.Set(context.Background(), c.namespaceOptionsHostIpc, hostIpc, nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Labels
	labels, err := json.Marshal(config.Labels)
	if err != nil {
		return err
	}
	_, err = kapi.Set(context.Background(), c.labelsKey, string(labels), nil)
	if err != nil {
		return err
	}

	// PodSandboxConfig.Annotations
	annotations, err := json.Marshal(config.Annotations)
	if err != nil {
		return err
	}
	_, err = kapi.Set(context.Background(), c.annotationsKey, string(annotations), nil)
	if err != nil {
		return err
	}

	return nil
}

func (c *sandboxConverter) etcdToSandboxMetadata() (*kubeapi.PodSandboxMetadata, error) {
	kapi, err := c.tool.keysAPITool.newKeysAPI()
	if err != nil {
		return nil, err
	}

	resp, err := kapi.Get(context.Background(), c.metadataNameKey, nil)
	if err != nil {
		return nil, err
	}
	metadataName := resp.Node.Value

	resp, err = kapi.Get(context.Background(), c.metadataNamespaceKey, nil)
	if err != nil {
		return nil, err
	}
	metadataNamespace := resp.Node.Value

	var metadataAttempt32Ptr *uint32
	resp, err = kapi.Get(context.Background(), c.metadataAttemptKey, nil)
	if err != nil {
		return nil, err
	}
	if resp.Node.Value != "" {
		metadataAttempt, err := strconv.ParseUint(resp.Node.Value, 10, 32)
		if err != nil {
			return nil, err
		}
		metadataAttempt32 := uint32(metadataAttempt)
		metadataAttempt32Ptr = &metadataAttempt32
	} else {
		metadataAttempt32Ptr = nil
	}

	return &kubeapi.PodSandboxMetadata{
		Name: &metadataName,
		// Uid:       &metadataUid,
		Uid:       &c.podId,
		Namespace: &metadataNamespace,
		Attempt:   metadataAttempt32Ptr,
	}, nil
}

func (c *sandboxConverter) etcdToSandboxStatus() (*kubeapi.PodSandboxStatus, error) {
	kapi, err := c.tool.keysAPITool.newKeysAPI()
	if err != nil {
		return nil, err
	}

	// PodSandboxStatus.Linux.Namespace.Options
	resp, err := kapi.Get(context.Background(), c.namespaceOptionsHostNetwork, nil)
	if err != nil {
		return nil, err
	}
	namespaceOptionsHostNetwork, err := strconv.ParseBool(resp.Node.Value)
	if err != nil {
		return nil, err
	}

	resp, err = kapi.Get(context.Background(), c.namespaceOptionsHostPid, nil)
	if err != nil {
		return nil, err
	}
	namespaceOptionsHostPid, err := strconv.ParseBool(resp.Node.Value)
	if err != nil {
		return nil, err
	}

	resp, err = kapi.Get(context.Background(), c.namespaceOptionsHostIpc, nil)
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

	// PodSandboxConfig.Labels
	var labels map[string]string
	resp, err = kapi.Get(context.Background(), c.labelsKey, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(resp.Node.Value), &labels); err != nil {
		return nil, err
	}

	// PodSandboxConfig.Annotations
	var annotations map[string]string
	resp, err = kapi.Get(context.Background(), c.annotationsKey, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(resp.Node.Value), &annotations); err != nil {
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
		Labels:      labels,
		Annotations: annotations,
	}

	return podSandboxStatus, nil
}

func (c *sandboxConverter) etcdToSandbox() (*kubeapi.PodSandbox, error) {
	kapi, err := c.tool.keysAPITool.newKeysAPI()
	if err != nil {
		return nil, err
	}

	podSandboxMetadata, err := c.etcdToSandboxMetadata()
	if err != nil {
		return nil, err
	}
	state := kubeapi.PodSandBoxState_READY
	createdAt := int64(0)

	var labels map[string]string
	resp, err := kapi.Get(context.Background(), c.labelsKey, nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(resp.Node.Value), &labels); err != nil {
		return nil, err
	}

	id := c.podId

	return &kubeapi.PodSandbox{
		Id:        &id,
		Metadata:  podSandboxMetadata,
		State:     &state,
		CreatedAt: &createdAt,
		Labels:    labels,
	}, nil
}

type SandboxTool struct {
	keysAPITool *KeysAPITool
}

func NewSandboxTool(keysAPITool *KeysAPITool) (*SandboxTool, error) {
	kapi, err := keysAPITool.newKeysAPI()
	if err != nil {
		return nil, err
	}
	if _, err = kapi.Set(context.Background(), "/sandbox", "", &etcd.SetOptions{Dir: true}); err != nil {
		// 102 "Not a file error" means that the dir node already exists.
		// There is no way to tell etcd client to ignore this fact.
		// TODO(nhlfr): Report a bug in etcd about that.
		if !strings.Contains(err.Error(), "102") {
			return nil, err
		}
	}
	return &SandboxTool{keysAPITool: keysAPITool}, nil
}

func (s *SandboxTool) CreatePodSandbox(config *kubeapi.PodSandboxConfig) error {
	podId := config.Metadata.GetUid()
	c := newSandboxConverter(s, podId)
	if err := c.sandboxConfigToEtcd(config); err != nil {
		return err
	}
	return nil
}

func (s *SandboxTool) PodSandboxStatus(podId string) (*kubeapi.PodSandboxStatus, error) {
	c := newSandboxConverter(s, podId)
	status, err := c.etcdToSandboxStatus()
	if err != nil {
		return nil, err
	}
	return status, nil
}

func filterPodSandbox(sandbox *kubeapi.PodSandbox, filter *kubeapi.PodSandboxFilter) bool {
	if filter == nil {
		return true
	}

	if filter.State != nil && *sandbox.State != *filter.State {
		return false
	}
	return true
}

func (s *SandboxTool) ListPodSandbox(filter *kubeapi.PodSandboxFilter) ([]*kubeapi.PodSandbox, error) {
	kapi, err := s.keysAPITool.newKeysAPI()
	if err != nil {
		return []*kubeapi.PodSandbox{}, err
	}

	resp, err := kapi.Get(context.Background(), "/sandbox", nil)
	if err != nil {
		return []*kubeapi.PodSandbox{}, err
	}

	podSandboxList := make([]*kubeapi.PodSandbox, 0, resp.Node.Nodes.Len())
	for _, node := range resp.Node.Nodes {
		keyPath := strings.Split(node.Key, "/")
		podId := keyPath[len(keyPath)-1]
		c := newSandboxConverter(s, podId)
		podSandbox, err := c.etcdToSandbox()
		if err != nil {
			return nil, err
		}
		if filterPodSandbox(podSandbox, filter) {
			podSandboxList = append(podSandboxList, podSandbox)
		}
	}

	return podSandboxList, nil
}
