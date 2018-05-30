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
	"reflect"
	"testing"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"

	"github.com/Mirantis/virtlet/pkg/metadata/fake"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/network"
	"github.com/Mirantis/virtlet/tests/criapi"
	"github.com/Mirantis/virtlet/tests/gm"
)

func TestConversions(t *testing.T) {
	configs := fake.GetSandboxes(2)
	criConfigs := criapi.GetSandboxes(2)
	criConfigWithAltLogDir := *criConfigs[0]
	criConfigWithAltLogDir.LogDirectory = "/some/pod/log/dir/" + criConfigWithAltLogDir.Metadata.Uid
	csn := &network.ContainerSideNetwork{
		NsPath: "/var/run/netns/bae464f1-6ee7-4ee2-826e-33293a9de95e",
	}
	podSandboxInfos := []interface{}{
		&types.PodSandboxInfo{
			PodID:     configs[0].Uid,
			Config:    configs[0],
			CreatedAt: 1496175540000000000,
			State:     types.PodSandboxState_SANDBOX_READY,
		},
		&types.PodSandboxInfo{
			PodID:     configs[1].Uid,
			Config:    configs[1],
			CreatedAt: 1496175550000000000,
			State:     types.PodSandboxState_SANDBOX_NOTREADY,
		},
	}
	containerInfos := []interface{}{
		&types.ContainerInfo{
			Id:        "f1bfb494-af3d-48ab-b8b1-2c850e1e8a00",
			Name:      "testcontainer",
			CreatedAt: 1496175540000000000,
			StartedAt: 1496175550000000000,
			State:     types.ContainerState_CONTAINER_CREATED,
			Config: types.VMConfig{
				PodSandboxID: configs[0].Uid,
				Image:        "testImage",
				ContainerLabels: map[string]string{
					"containerLabel": "foo",
				},
				ContainerAnnotations: map[string]string{
					"containerAnnotation": "foobar",
				},
				LogPath: "testcontainer_0.log",
			},
		},
		&types.ContainerInfo{
			Id:        "13bdedae-540d-4131-959b-366c6343d5b4",
			Name:      "testcontainer1",
			CreatedAt: 1496175560000000000,
			StartedAt: 1496175570000000000,
			State:     types.ContainerState_CONTAINER_EXITED,
			Config: types.VMConfig{
				PodSandboxID: configs[1].Uid,
				Image:        "testImage1",
				ContainerLabels: map[string]string{
					"containerLabel": "foo1",
				},
				ContainerAnnotations: map[string]string{
					"containerAnnotation": "foobar1",
				},
				LogPath: "testcontainer1_0.log",
			},
		},
	}
	for _, tc := range []struct {
		name      string
		converter interface{}
		in        []interface{}
	}{
		{
			name:      "PodSandboxInfoToCRIPodSandboxStatus",
			converter: PodSandboxInfoToCRIPodSandboxStatus,
			in:        podSandboxInfos,
		},
		{
			name:      "PodSandboxInfoToCRIPodSandbox",
			converter: PodSandboxInfoToCRIPodSandbox,
			in:        podSandboxInfos,
		},
		{
			name:      "CRIPodSandboxConfigToPodSandboxConfig",
			converter: CRIPodSandboxConfigToPodSandboxConfig,
			in: []interface{}{
				criConfigs[0],
				criConfigs[1],
			},
		},
		{
			name:      "CRIPodSandboxFilterToPodSandboxFilter",
			converter: CRIPodSandboxFilterToPodSandboxFilter,
			in: []interface{}{
				(*kubeapi.PodSandboxFilter)(nil),
				&kubeapi.PodSandboxFilter{},
				&kubeapi.PodSandboxFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
				},
				&kubeapi.PodSandboxFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
					State: &kubeapi.PodSandboxStateValue{
						kubeapi.PodSandboxState_SANDBOX_NOTREADY,
					},
				},
				&kubeapi.PodSandboxFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
					State: &kubeapi.PodSandboxStateValue{
						kubeapi.PodSandboxState_SANDBOX_READY,
					},
				},
				&kubeapi.PodSandboxFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
					State: &kubeapi.PodSandboxStateValue{
						kubeapi.PodSandboxState_SANDBOX_READY,
					},
					LabelSelector: map[string]string{
						"foo": "bar",
					},
				},
			},
		},
		{
			name: "GetVMConfig",
			converter: func(in *kubeapi.CreateContainerRequest) *types.VMConfig {
				r, err := GetVMConfig(in, csn)
				if err != nil {
					t.Fatalf("bad CreateContainerRequest: %#v", in)
				}
				return r
			},
			in: []interface{}{
				&kubeapi.CreateContainerRequest{
					PodSandboxId: criConfigs[0].Metadata.Uid,
					Config: &kubeapi.ContainerConfig{
						Image:  &kubeapi.ImageSpec{Image: "testImage"},
						Labels: map[string]string{"foo": "bar", "fizz": "buzz"},
						Metadata: &kubeapi.ContainerMetadata{
							Name: "testcontainer",
						},
					},
					SandboxConfig: criConfigs[0],
				},
				&kubeapi.CreateContainerRequest{
					PodSandboxId: criConfigs[0].Metadata.Uid,
					Config: &kubeapi.ContainerConfig{
						Image:  &kubeapi.ImageSpec{Image: "testImage"},
						Labels: map[string]string{"foo": "bar", "fizz": "buzz"},
						Metadata: &kubeapi.ContainerMetadata{
							Name: "testcontainer",
						},
						LogPath: "some_logpath_0.log",
					},
					SandboxConfig: &criConfigWithAltLogDir,
				},
				&kubeapi.CreateContainerRequest{
					PodSandboxId: criConfigs[1].Metadata.Uid,
					Config: &kubeapi.ContainerConfig{
						Image:  &kubeapi.ImageSpec{Image: "testImage"},
						Labels: map[string]string{"foo": "bar", "fizz": "buzz"},
						Mounts: []*kubeapi.Mount{
							{
								ContainerPath: "/mnt",
								HostPath:      "/whatever",
							},
						},
						Metadata: &kubeapi.ContainerMetadata{
							Name: "testcontainer",
						},
					},
					SandboxConfig: criConfigs[1],
				},
			},
		},
		{
			name:      "CRIContainerFilterToContainerFilter",
			converter: CRIContainerFilterToContainerFilter,
			in: []interface{}{
				(*kubeapi.ContainerFilter)(nil),
				&kubeapi.ContainerFilter{},
				&kubeapi.ContainerFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
				},
				&kubeapi.ContainerFilter{
					PodSandboxId: "c5134b10-6474-4139-8bf9-f6f61a1f5906",
				},
				&kubeapi.ContainerFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
					State: &kubeapi.ContainerStateValue{
						kubeapi.ContainerState_CONTAINER_CREATED,
					},
				},
				&kubeapi.ContainerFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
					State: &kubeapi.ContainerStateValue{
						kubeapi.ContainerState_CONTAINER_RUNNING,
					},
				},
				&kubeapi.ContainerFilter{
					Id: "a393e311-5f4f-402b-8567-864d2ab81b83",
					State: &kubeapi.ContainerStateValue{
						kubeapi.ContainerState_CONTAINER_EXITED,
					},
					LabelSelector: map[string]string{
						"foo": "bar",
					},
				},
			},
		},
		{
			name:      "ContainerInfoToCRIContainer",
			converter: ContainerInfoToCRIContainer,
			in:        containerInfos,
		},
		{
			name:      "ContainerInfoToCRIContainerStatus",
			converter: ContainerInfoToCRIContainerStatus,
			in:        containerInfos,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conv := reflect.ValueOf(tc.converter)
			var r []map[string]interface{}
			for _, v := range tc.in {
				rv := conv.Call([]reflect.Value{reflect.ValueOf(v)})
				if len(rv) != 1 {
					t.Fatalf("more than one value returned by the converter for %#v", v)
				}
				r = append(r, map[string]interface{}{
					"in":  v,
					"out": rv[0].Interface(),
				})
			}
			gm.Verify(t, gm.NewYamlVerifier(r))
		})
	}
}
