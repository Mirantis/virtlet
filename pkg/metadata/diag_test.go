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

package metadata

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/Mirantis/virtlet/pkg/metadata/fake"
	"github.com/Mirantis/virtlet/tests/gm"
)

func verifyMetadataDump(t *testing.T, store Store) {
	dr, err := GetMetadataDumpSource(store).DiagnosticInfo()
	switch {
	case err != nil:
		t.Fatalf("DiagnosticInfo(): %v", err)
	case dr.IsDir:
		t.Error("metadata dump result isn't expected to be a directory")
	case dr.Ext != "txt":
		t.Error("bad metadata dump ext")
	}
	gm.Verify(t, dr.Data)
}

func TestDumpMetadata(t *testing.T) {
	fakeClock := clockwork.NewFakeClockAt(time.Date(2018, 7, 9, 19, 25, 0, 0, time.UTC))
	sandboxes := fake.GetSandboxes(2)
	containers := fake.GetContainersConfig(sandboxes)
	store := setUpTestStore(t, sandboxes, containers, fakeClock)
	verifyMetadataDump(t, store)
}

func TestDumpEmptyMetadata(t *testing.T) {
	store := setUpTestStore(t, nil, nil, nil)
	verifyMetadataDump(t, store)
}
