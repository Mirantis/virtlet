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

package manager

import (
	"errors"
	"fmt"
	"path/filepath"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/network"
)

func podSandboxMetadata(in *types.PodSandboxInfo) *kubeapi.PodSandboxMetadata {
	return &kubeapi.PodSandboxMetadata{
		Name:      in.Config.Name,
		Uid:       in.PodID,
		Namespace: in.Config.Namespace,
		Attempt:   in.Config.Attempt,
	}
}

// PodSandboxInfoToCRIPodSandboxStatus converts PodSandboxInfo to CRI PodSandboxStatus.
func PodSandboxInfoToCRIPodSandboxStatus(in *types.PodSandboxInfo) *kubeapi.PodSandboxStatus {
	return &kubeapi.PodSandboxStatus{
		Id:        in.PodID,
		Metadata:  podSandboxMetadata(in),
		State:     kubeapi.PodSandboxState(in.State),
		CreatedAt: in.CreatedAt,
		Linux: &kubeapi.LinuxPodSandboxStatus{
			Namespaces: &kubeapi.Namespace{
				// pod namespace for net / pid / ipc
				Options: &kubeapi.NamespaceOption{},
			},
		},
		Labels:      in.Config.Labels,
		Annotations: in.Config.Annotations,
	}
}

// PodSandboxInfoToCRIPodSandbox converts PodSandboxInfo to CRI PodSandbox.
func PodSandboxInfoToCRIPodSandbox(in *types.PodSandboxInfo) *kubeapi.PodSandbox {
	return &kubeapi.PodSandbox{
		Id:          in.PodID,
		Metadata:    podSandboxMetadata(in),
		State:       kubeapi.PodSandboxState(in.State),
		CreatedAt:   in.CreatedAt,
		Labels:      in.Config.Labels,
		Annotations: in.Config.Annotations,
	}
}

// CRIPodSandboxConfigToPodSandboxConfig converts CRI PodSandboxConfig to PodSandboxConfig.
func CRIPodSandboxConfigToPodSandboxConfig(in *kubeapi.PodSandboxConfig) *types.PodSandboxConfig {
	meta := in.GetMetadata()
	var portMappings []*types.PortMapping
	for _, pm := range in.GetPortMappings() {
		portMappings = append(portMappings, &types.PortMapping{
			Protocol:      types.Protocol(pm.Protocol),
			ContainerPort: pm.ContainerPort,
			HostPort:      pm.HostPort,
			HostIp:        pm.HostIp,
		})
	}
	return &types.PodSandboxConfig{
		Name:         meta.GetName(),
		Uid:          meta.GetUid(),
		Namespace:    meta.GetNamespace(),
		Attempt:      meta.GetAttempt(),
		Hostname:     in.GetHostname(),
		LogDirectory: in.GetLogDirectory(),
		DnsConfig: &types.DNSConfig{
			Servers:  in.GetDnsConfig().GetServers(),
			Searches: in.GetDnsConfig().GetSearches(),
			Options:  in.GetDnsConfig().GetOptions(),
		},
		PortMappings: portMappings,
		Labels:       in.GetLabels(),
		Annotations:  in.GetAnnotations(),
		CgroupParent: in.GetLinux().GetCgroupParent(),
	}
}

// CRIPodSandboxFilterToPodSandboxFilter converts CRI PodSandboxFilter to PodSandboxFilter.
func CRIPodSandboxFilterToPodSandboxFilter(in *kubeapi.PodSandboxFilter) *types.PodSandboxFilter {
	if in == nil {
		return nil
	}
	var state *types.PodSandboxState
	if in.State != nil {
		state = (*types.PodSandboxState)(&in.State.State)
	}
	return &types.PodSandboxFilter{
		Id:            in.Id,
		State:         state,
		LabelSelector: in.LabelSelector,
	}
}

// GetVMConfig translates CRI CreateContainerRequest and CNI info to a VMConfig.
func GetVMConfig(in *kubeapi.CreateContainerRequest, csn *network.ContainerSideNetwork) (*types.VMConfig, error) {
	if in.Config == nil || in.Config.Metadata == nil || in.Config.Metadata.Name == "" || in.Config.Image == nil || in.SandboxConfig == nil || in.SandboxConfig.Metadata == nil {
		return nil, errors.New("invalid input data")
	}

	// Note that the fallbacks used belog for log dir & path
	// shouldn't actually be used for real kubelet.
	logDir := in.SandboxConfig.LogDirectory
	if logDir == "" {
		logDir = fmt.Sprintf("/var/log/pods/%s", in.PodSandboxId)
	}

	logPath := in.Config.LogPath
	if logPath == "" {
		logPath = fmt.Sprintf("%s_%d.log", in.Config.Metadata.Name, in.Config.Metadata.Attempt)
	}
	r := &types.VMConfig{
		PodSandboxID:         in.PodSandboxId,
		PodName:              in.SandboxConfig.Metadata.Name,
		PodNamespace:         in.SandboxConfig.Metadata.Namespace,
		Name:                 in.Config.Metadata.Name,
		Image:                in.Config.Image.Image,
		Attempt:              in.Config.Metadata.Attempt,
		PodAnnotations:       in.SandboxConfig.Annotations,
		ContainerAnnotations: in.Config.Annotations,
		ContainerLabels:      in.Config.Labels,
		ContainerSideNetwork: csn,
		LogDirectory:         logDir,
		LogPath:              logPath,
	}

	if linuxCfg := in.Config.Linux; linuxCfg != nil && linuxCfg.Resources != nil {
		res := linuxCfg.Resources
		r.MemoryLimitInBytes = res.MemoryLimitInBytes
		r.CPUShares = res.CpuShares
		r.CPUPeriod = res.CpuPeriod
		r.CPUQuota = res.CpuQuota
	}

	for _, entry := range in.Config.Envs {
		r.Environment = append(r.Environment, types.VMKeyValue{Key: entry.Key, Value: entry.Value})
	}

	for _, mount := range in.Config.Mounts {
		r.Mounts = append(r.Mounts, types.VMMount{
			ContainerPath: mount.ContainerPath,
			HostPath:      mount.HostPath,
			Readonly:      mount.Readonly,
		})
	}

	for _, dev := range in.Config.Devices {
		r.VolumeDevices = append(r.VolumeDevices, types.VMVolumeDevice{
			DevicePath: dev.ContainerPath,
			HostPath:   dev.HostPath,
		})
	}

	return r, nil
}

// CRIContainerFilterToContainerFilter converts CRI ContainerFilter to ContainerFilter.
func CRIContainerFilterToContainerFilter(in *kubeapi.ContainerFilter) *types.ContainerFilter {
	if in == nil {
		return nil
	}
	var state *types.ContainerState
	if in.State != nil {
		state = (*types.ContainerState)(&in.State.State)
	}
	return &types.ContainerFilter{
		Id:            in.Id,
		State:         state,
		PodSandboxID:  in.PodSandboxId,
		LabelSelector: in.LabelSelector,
	}
}

func containerMetadata(in *types.ContainerInfo) *kubeapi.ContainerMetadata {
	return &kubeapi.ContainerMetadata{
		Name:    in.Name,
		Attempt: in.Config.Attempt,
	}
}

// ContainerInfoToCRIContainer converts ContainerInfo to CRI Container
func ContainerInfoToCRIContainer(in *types.ContainerInfo) *kubeapi.Container {
	return &kubeapi.Container{
		Id:           in.Id,
		PodSandboxId: in.Config.PodSandboxID,
		Metadata:     containerMetadata(in),
		Image:        &kubeapi.ImageSpec{Image: in.Config.Image},
		ImageRef:     in.Config.Image,
		State:        kubeapi.ContainerState(in.State),
		CreatedAt:    in.CreatedAt,
		Labels:       in.Config.ContainerLabels,
		Annotations:  in.Config.ContainerAnnotations,
	}
}

// ContainerInfoToCRIContainerStatus convers ContainerInfo to CRI ContainerStatus.
func ContainerInfoToCRIContainerStatus(in *types.ContainerInfo) *kubeapi.ContainerStatus {
	var mounts []*kubeapi.Mount
	for _, m := range in.Config.Mounts {
		mounts = append(mounts, &kubeapi.Mount{
			ContainerPath: m.ContainerPath,
			HostPath:      m.HostPath,
			Readonly:      m.Readonly,
		})
	}
	return &kubeapi.ContainerStatus{
		Id:          in.Id,
		Metadata:    containerMetadata(in),
		Image:       &kubeapi.ImageSpec{Image: in.Config.Image},
		ImageRef:    in.Config.Image,
		State:       kubeapi.ContainerState(in.State),
		CreatedAt:   in.CreatedAt,
		StartedAt:   in.StartedAt,
		Labels:      in.Config.ContainerLabels,
		Annotations: in.Config.ContainerAnnotations,
		Mounts:      mounts,
		LogPath:     filepath.Join(in.Config.LogDirectory, in.Config.LogPath),
		// TODO: FinishedAt, Reason, Message
	}
}
