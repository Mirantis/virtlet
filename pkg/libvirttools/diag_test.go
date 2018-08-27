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

package libvirttools

import (
	"testing"

	fakemeta "github.com/Mirantis/virtlet/pkg/metadata/fake"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/tests/gm"
)

func TestDump(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder())
	defer ct.teardown()

	sandbox := fakemeta.GetSandboxes(1)[0]
	ct.setPodSandbox(sandbox)

	ct.createContainer(sandbox, nil, nil)

	// Avoid having volatile cloud-init .iso path in the domain
	// definition
	ct.domainConn.UseNonVolatileDomainDef()

	src := NewLibvirtDiagSource(ct.domainConn, ct.storageConn)
	dr, err := src.DiagnosticInfo()
	if err != nil {
		t.Fatalf("DiagnosticInfo(): %v", err)
	}
	gm.Verify(t, gm.NewYamlVerifier(dr))
}
