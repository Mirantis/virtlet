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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

const (
	tempFileSizeInKB = 2048
)

type loopbackHandler struct {
	t        *testing.T
	filePath string
	devPath  string
}

func prepareLoopbackDevice(t *testing.T) *loopbackHandler {
	lh := loopbackHandler{
		t:        t,
		filePath: createTemporaryFile(),
	}
	lh.Attach()
	return &lh
}

func (lh *loopbackHandler) Cleanup() {
	defer func() {
		if err := os.Remove(lh.filePath); err != nil {
			log.Fatalf("Can not unlink temporary file: %v", err)
		}
	}()
	lh.Detach()
}

func TestRawDevices(t *testing.T) {
	l := prepareLoopbackDevice(t)
	defer l.Cleanup()

	ct := newContainerTester(t)
	defer ct.teardown()
	imageSpec := ct.imageSpecs[0]
	sandbox := ct.sandboxes[0]
	container := ct.containers[0]

	ct.mountFlexvolume(ct.sandboxes[0].Metadata.Uid, "vol", map[string]interface{}{
		"type": "raw",
		"path": l.devPath,
	})

	ct.pullImage(imageSpec)
	ct.runPodSandbox(sandbox)
	ct.createContainer(sandbox, container, imageSpec, nil)
	ct.startContainer(container.ContainerId)

	ct.verifyContainerState(container.ContainerId, container.Name, kubeapi.ContainerState_CONTAINER_RUNNING)

	// check for loop in container dom
	cmd := fmt.Sprintf("virsh domblklist %s | grep '/dev/loop' | wc -l", container.ContainerId)
	verifyUsingShell(t, cmd, "the number of loop devices attached", "1")

	ct.stopContainer(container.ContainerId)
	ct.removeContainer(container.ContainerId)
	ct.verifyNoContainers(nil)
}

func createTemporaryFile() string {
	file, err := ioutil.TempFile(os.TempDir(), "loopback_test_")
	if err != nil {
		log.Fatalf("Can not create temporary file: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Fatalf("Can not close temporary file: %v", err)
		}
	}()

	zeros := make([]byte, 1024)
	for counter := 0; counter < tempFileSizeInKB; counter++ {
		if _, err := file.Write(zeros); err != nil {
			log.Fatalf("Error writing temporary file: %v", err)
		}
	}

	return file.Name()
}

func (lh *loopbackHandler) Attach() {
	lh.devPath = runShellCommand(lh.t, "losetup -f %s --show", lh.filePath)
}

func (lh *loopbackHandler) Detach() {
	runShellCommand(lh.t, "losetup -d %s", lh.devPath)
}
