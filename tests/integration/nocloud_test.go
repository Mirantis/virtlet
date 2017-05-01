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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Mirantis/virtlet/pkg/flexvolume"
	"github.com/Mirantis/virtlet/pkg/utils"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

const (
	noCloudMetaData = `
instance-id: some-instance-id
local-hostname: foobar.example.com
`
	noCloudUserData = `
    #cloud-config
    fqdn: foobar.example.com
`
)

func TestCloudInitNoCloud(t *testing.T) {
	ct := newContainerTester(t)
	defer ct.teardown()
	imageSpec := ct.imageSpecs[0]
	sandbox := ct.sandboxes[0]
	container := ct.containers[0]

	podDir := fmt.Sprintf("/var/lib/kubelet/pods/%s", sandbox.Metadata.Uid)
	volumeDir := filepath.Join(podDir, "volumes/virtlet~flexvolume_driver/nocloud")
	if err := os.MkdirAll(volumeDir, 0755); err != nil {
		t.Fatalf("can't create volume dir: %v", err)
	}
	defer os.RemoveAll(podDir)

	// Here we simulate what kubelet is doing by involing our flexvolume
	// driver directly.
	// XXX: there's a subtle difference between what we do here and
	// what happens on the real system though. In the latter case
	// virtlet pod doesn't see the contents of tmpfs because hostPath volumes
	// are mounted privately into the virtlet pod mount ns. Here we
	// let Virtlet process tmpfs contents. Currently the contents
	// of flexvolume's tmpfs and the shadowed directory should be the
	// same though.
	fv := flexvolume.NewFlexVolumeDriver(func() string {
		return "abb67e3c-71b3-4ddd-5505-8c4215d5c4eb"
	}, flexvolume.NewLinuxMounter())
	noCloudJsonOpts := utils.MapToJson(map[string]interface{}{
		"type":     "nocloud",
		"metadata": noCloudMetaData,
		"userdata": noCloudUserData,
	})
	r := fv.Run([]string{"mount", volumeDir, noCloudJsonOpts})
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(r), &m); err != nil {
		t.Fatalf("failed to unmarshal flexvolume mount result: %v", err)
	}
	if m["status"] != "Success" {
		t.Fatalf("flexvolume mount failed, result: %s", r)
	}
	defer func() {
		r := fv.Run([]string{"unmount", volumeDir})
		if err := json.Unmarshal([]byte(r), &m); err != nil {
			t.Fatalf("failed to unmarshal flexvolume unmount result: %v", err)
		}
		if m["status"] != "Success" {
			t.Fatalf("flexvolume unmount failed, result: %s", r)
		}

	}()

	ct.pullImage(imageSpec)
	ct.runPodSandbox(sandbox)
	ct.createContainer(sandbox, container, imageSpec, nil)
	ct.startContainer(container.ContainerId)

	ct.waitForContainerRunning(container.ContainerId, container.Name)

	isoPath := runShellCommand(t, "virsh domblklist $(virsh list --name)|grep -o '/var/lib/kubelet.*\\.iso'")
	files, err := testutils.IsoToMap(isoPath)
	if err != nil {
		t.Fatalf("isoToMap() on %q: %v", isoPath, err)
	}
	expectedFiles := map[string]interface{}{
		"meta-data": noCloudMetaData,
		"user-data": noCloudUserData,
	}
	if !reflect.DeepEqual(files, expectedFiles) {
		t.Errorf("bad nocloud metadata iso:\n%s\n-- instead of --\n%s", utils.MapToJson(files), utils.MapToJson(expectedFiles))
	}

	ct.stopContainer(container.ContainerId)
	ct.removeContainer(container.ContainerId)
	ct.verifyNoContainers(nil)
}
