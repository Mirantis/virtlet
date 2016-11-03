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

package integration

import (
	"testing"

	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func TestContainerCreate(t *testing.T) {
	manager := NewVirtletManager()
	if err := manager.Run(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	runtimeServiceClient := kubeapi.NewRuntimeServiceClient(manager.conn)

	// Sandbox request
	hostNetwork := false
	hostPid := false
	hostIpc := false
	namespaceOptions := &kubeapi.NamespaceOption{
		HostNetwork: &hostNetwork,
		HostPid:     &hostPid,
		HostIpc:     &hostIpc,
	}

	cgroupParent := ""
	linuxPodSandboxConfig := &kubeapi.LinuxPodSandboxConfig{
		CgroupParent:     &cgroupParent,
		NamespaceOptions: namespaceOptions,
	}

	podSandboxName := "foo"
	podSandboxUid := "c8e21c1b-8008-4337-ac16-f70f2dfaf101"
	podSandboxNamespace := "default"
	podSandboxMetadata := &kubeapi.PodSandboxMetadata{
		Name:      &podSandboxName,
		Uid:       &podSandboxUid,
		Namespace: &podSandboxNamespace,
	}

	podSandboxHostname := "localhost"

	podSandboxConfig := &kubeapi.PodSandboxConfig{
		Metadata: podSandboxMetadata,
		Hostname: &podSandboxHostname,
		Linux:    linuxPodSandboxConfig,
	}
	sandboxIn := &kubeapi.RunPodSandboxRequest{Config: podSandboxConfig}

	sandboxOut, err := runtimeServiceClient.RunPodSandbox(context.Background(), sandboxIn)
	if err != nil {
		t.Fatal(err)
	}

	// Container request
	imageSpec := &kubeapi.ImageSpec{Image: &imageUrl}
	hostPath := "/var/lib/virtlet"
	config := &kubeapi.ContainerConfig{
		Image:  imageSpec,
		Mounts: []*kubeapi.Mount{&kubeapi.Mount{HostPath: &hostPath}},
	}
	containerIn := &kubeapi.CreateContainerRequest{
		PodSandboxId:  sandboxOut.PodSandboxId,
		Config:        config,
		SandboxConfig: podSandboxConfig,
	}

	if _, err := runtimeServiceClient.CreateContainer(context.Background(), containerIn); err != nil {
		t.Fatal(err)
	}
}
