/*
Copyright 2019 Mirantis

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
	"fmt"
	"testing"

	fakemeta "github.com/Mirantis/virtlet/pkg/metadata/fake"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/tests/gm"
)

func TestUpdateCpusets(t *testing.T) {
	files := map[string]string{
		"/proc/4242/cgroup": "3:cpuset:/somepath/in/cgroups/emulator\n",
	}
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil, files)
	defer ct.teardown()

	sandbox := fakemeta.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)
	containerID := ct.createContainer(sandbox, nil, nil)
	pidFilePath := fmt.Sprintf("/run/libvirt/qemu/virtlet-%s-%s.pid", containerID[:13], sandbox.Name)
	files[pidFilePath] = "4242"

	ct.rec.Rec("Calling setting cpuset for emulator proces", nil)
	ct.virtTool.UpdateCpusetsForEmulatorProcess(containerID, "42")

	ct.rec.Rec("Calling setting cpuset for domain definition", nil)
	ct.virtTool.UpdateCpusetsInContainerDefinition(containerID, "42")

	ct.rec.Rec("Invoking RemoveContainer()", nil)
	ct.removeContainer(containerID)
	gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
}
