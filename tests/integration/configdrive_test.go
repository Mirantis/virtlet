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

package integration

import (
	"reflect"
	"testing"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"

	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

const (
	configDriveMetaData      = "{\"hostname\":\"testName_0\",\"instance-id\":\"testName_0.default\",\"local-hostname\":\"testName_0\",\"uuid\":\"testName_0.default\"}"
	configDriveUserData      = "#cloud-config\n"
	configDriveNetworkConfig = "{}"
)

func TestCloudInitConfigDrive(t *testing.T) {
	ct := newContainerTester(t)
	defer ct.teardown()
	imageSpec := ct.imageSpecs[0]
	sandbox := ct.sandboxes[0]
	sandbox.Annotations["VirtletCloudInitImageType"] = "configdrive"
	container := ct.containers[0]

	ct.pullImage(imageSpec)
	ct.runPodSandbox(sandbox)
	ct.createContainer(sandbox, container, imageSpec, nil)
	ct.startContainer(container.ContainerID)

	ct.verifyContainerState(container.ContainerID, container.Name, kubeapi.ContainerState_CONTAINER_RUNNING)

	isoPath := runShellCommand(t, `virsh domblklist $(virsh list --name)|grep -o '/.*config-.*\.iso[^ ]*'`)
	files, err := testutils.IsoToMap(isoPath)
	if err != nil {
		t.Fatalf("isoToMap() on %q: %v", isoPath, err)
	}
	expectedFiles := map[string]interface{}{
		"openstack": map[string]interface{}{
			"latest": map[string]interface{}{
				"meta_data.json":    configDriveMetaData,
				"network_data.json": configDriveNetworkConfig,
				"user_data":         configDriveUserData,
			},
		},
	}
	if !reflect.DeepEqual(files, expectedFiles) {
		t.Errorf("bad config drive iso:\n%s\n-- instead of --\n%s", utils.MapToJSON(files), utils.MapToJSON(expectedFiles))
	}

	ct.stopContainer(container.ContainerID)
	ct.removeContainer(container.ContainerID)
	ct.verifyNoContainers(nil)
}
