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
	"os/exec"
	"strings"
	"testing"

	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

const (
	tempFileSizeInKB = 2048
)

type loopbackHandler struct {
	filePath string
	devPath  string
}

func prepareLoopbackDevice() *loopbackHandler {
	lh := loopbackHandler{
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
	l := prepareLoopbackDevice()
	defer l.Cleanup()

	ct := newContainerTester(t)
	defer ct.teardown()
	imageSpec := ct.imageSpecs[0]
	sandbox := ct.sandboxes[0]
	container := ct.containers[0]

	sandbox.Annotations["VirtletVolumes"] = fmt.Sprintf(`[{"Name": "vol", "Format": "raw", "Path": "%s"}]`, l.devPath)

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

	ct.waitForContainerRunning(container.ContainerId, container.Name)
	ct.stopContainer(container.ContainerId)
	ct.removeContainer(container.ContainerId)
	ct.waitForNoContainers(&kubeapi.ContainerFilter{
		Id: container.ContainerId,
	})
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
			log.Fatalf("Error during filling temporary file: %v", err)
		}
	}

	return file.Name()
}

func fromShell(format string, a ...interface{}) string {
	command := fmt.Sprintf(format, a...)
	out, err := exec.Command("bash", "-c", command).Output()
	if err != nil {
		log.Fatalf("Error during execution of command '%s': %v", command, err)
	}
	return strings.TrimSpace(string(out))
}

func (lh *loopbackHandler) Attach() {
	lh.devPath = fromShell("losetup -f %s --show", lh.filePath)
}

func (lh *loopbackHandler) Detach() {
	_ = fromShell("losetup -d %s", lh.devPath)
}
