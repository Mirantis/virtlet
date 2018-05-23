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

func TestContainerStatuses(t *testing.T) {
	ct := newContainerTester(t)
	defer ct.teardown()
	imageSpec := ct.imageSpecs[0]
	sandbox := ct.sandboxes[0]
	container := ct.containers[0]
	ct.pullImage(imageSpec)
	ct.runPodSandbox(sandbox)
	ct.createContainer(sandbox, container, imageSpec, nil)
	ct.verifyContainerState(container.ContainerID, container.Name, kubeapi.ContainerState_CONTAINER_CREATED)
	ct.startContainer(container.ContainerID)

	listResp := ct.listContainers(&kubeapi.ContainerFilter{
		Id: container.ContainerID,
	})
	if len(listResp.Containers) != 1 {
		t.Errorf("Expected single container, instead got: %d", len(listResp.Containers))
	} else if listResp.Containers[0].Id != container.ContainerID {
		t.Errorf("Didn't find expected container id %s in returned containers list %v", container.ContainerID, listResp.Containers)
	}

	ct.verifyContainerState(container.ContainerID, container.Name, kubeapi.ContainerState_CONTAINER_RUNNING)
	ct.stopContainer(container.ContainerID)
	ct.verifyContainerState(container.ContainerID, container.Name, kubeapi.ContainerState_CONTAINER_EXITED)
	ct.removeContainer(container.ContainerID)
	ct.verifyNoContainers(nil)
}
