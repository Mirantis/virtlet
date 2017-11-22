/*
Copyright 2016-2017 Mirantis

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

package libvirttools

import (
	"errors"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

// TODO: this file should be moved to 'pkg/manager' at some point
// under a different name

// GetVMConfig translates CRI's CreateContainerRequest and CNI info to a VMConfig
func GetVMConfig(in *kubeapi.CreateContainerRequest, cniConfig string) (*VMConfig, error) {
	if in.Config == nil || in.Config.Metadata == nil || in.Config.Metadata.Name == "" || in.Config.Image == nil || in.SandboxConfig == nil || in.SandboxConfig.Metadata == nil {
		return nil, errors.New("invalid input data")
	}

	r := &VMConfig{
		PodSandboxId:         in.PodSandboxId,
		PodName:              in.SandboxConfig.Metadata.Name,
		PodNamespace:         in.SandboxConfig.Metadata.Namespace,
		Name:                 in.Config.Metadata.Name,
		Image:                in.Config.Image.Image,
		Attempt:              in.Config.Metadata.Attempt,
		PodAnnotations:       in.SandboxConfig.Annotations,
		ContainerAnnotations: in.Config.Annotations,
		ContainerLabels:      in.Config.Labels,
		CNIConfig:            cniConfig,
	}

	if linuxCfg := in.Config.Linux; linuxCfg != nil && linuxCfg.Resources != nil {
		res := linuxCfg.Resources
		r.MemoryLimitInBytes = res.MemoryLimitInBytes
		r.CpuShares = res.CpuShares
		r.CpuPeriod = res.CpuPeriod
		r.CpuQuota = res.CpuQuota
	}

	for _, entry := range in.Config.Envs {
		r.Environment = append(r.Environment, &VMKeyValue{Key: entry.Key, Value: entry.Value})
	}

	for _, mount := range in.Config.Mounts {
		r.Mounts = append(r.Mounts, &VMMount{
			ContainerPath: mount.ContainerPath,
			HostPath:      mount.HostPath,
		})
	}

	return r, nil
}
