/*
Copyright 2016-2017 Mirantis

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

package manager

import (
	"testing"

	"github.com/davecgh/go-spew/spew"

	"github.com/Mirantis/virtlet/tests/criapi"
)

func TestPodSanboxConfigValidation(t *testing.T) {
	invalidSandboxes := criapi.GetSandboxes(1)

	// Now let's make generated configs to be invalid
	invalidSandboxes[0].Metadata = nil

	if err := validatePodSandboxConfig(invalidSandboxes[0]); err == nil {
		t.Errorf("Invalid pod sandbox passed validation:\n%s", spew.Sdump(invalidSandboxes[0]))
	}
}
