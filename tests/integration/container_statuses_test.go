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

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func TestContainerStatuses(t *testing.T) {
	ct := newContainerTester(t)
	defer ct.teardown()
	imageSpec := ct.imageSpecs[0]
	sandbox := ct.sandboxes[0]
	container := ct.containers[0]
	ct.pullImage(imageSpec)
	ct.runPodSandbox(sandbox)
	ct.createContainer(sandbox, container, imageSpec, nil)
	ct.startContainer(container.ContainerId)

	listResp := ct.listContainers(&kubeapi.ContainerFilter{
		Id: container.ContainerId,
	})
	if len(listResp.Containers) != 1 {
		t.Errorf("Expected single container, instead got: %d", len(listResp.Containers))
	}
	if listResp.Containers[0].Id != container.ContainerId {
		t.Errorf("Didn't find expected container id %s in returned containers list %v", container.ContainerId, listResp.Containers)
	}

	ct.waitForContainerRunning(container.ContainerId)
	ct.stopContainer(container.ContainerId)
	ct.removeContainer(container.ContainerId)
	ct.waitForNoContainers(&kubeapi.ContainerFilter{
		Id: container.ContainerId,
	})
}
