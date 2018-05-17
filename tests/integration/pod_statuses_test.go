/*
Copyright 2017 Mirantis

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

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

func TestPodStatuses(t *testing.T) {
	ct := newContainerTester(t)
	defer ct.teardown()
	imageSpec := ct.imageSpecs[0]
	sandbox := ct.sandboxes[0]
	container := ct.containers[0]
	ct.pullImage(imageSpec)
	ct.runPodSandbox(sandbox)
	ct.verifyPodSandboxState(sandbox, kubeapi.PodSandboxState_SANDBOX_READY)
	ct.createContainer(sandbox, container, imageSpec, nil)
	ct.verifyPodSandboxState(sandbox, kubeapi.PodSandboxState_SANDBOX_READY)
	ct.startContainer(container.ContainerID)
	ct.verifyPodSandboxState(sandbox, kubeapi.PodSandboxState_SANDBOX_READY)
	ct.stopContainer(container.ContainerID)
	ct.removeContainer(container.ContainerID)
	ct.stopPodSandbox(sandbox.Metadata.Uid)
	ct.verifyPodSandboxState(sandbox, kubeapi.PodSandboxState_SANDBOX_NOTREADY)
	// make sure stopping sandbox is idempotent
	ct.stopPodSandbox(sandbox.Metadata.Uid)
	ct.verifyPodSandboxState(sandbox, kubeapi.PodSandboxState_SANDBOX_NOTREADY)
	ct.removePodSandbox(sandbox.Metadata.Uid)
	if len(ct.listPodSandbox(nil)) > 0 {
		t.Errorf("pod sandbox still returned from ListPodSandbox() after removal")
	}
}
