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

package libvirttools

import (
	"testing"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt/fake"
	"github.com/Mirantis/virtlet/tests/gm"
)

var (
	randomUUIDs = [...]string{
		"5edfe2ad-9852-439b-bbfb-3fe8b7c72906",
		"8a6163c3-e4ee-488f-836a-d2abe92d0744",
		"13f51f8d-0f4e-4538-9db0-413380ff9c84",
	}
)

func TestDomainCleanup(t *testing.T) {
	ct := newContainerTester(t, fake.NewToplevelRecorder())
	defer ct.teardown()

	for _, uuid := range randomUUIDs {
		if _, err := ct.domainConn.DefineDomain(&libvirtxml.Domain{
			Name: "virtlet-" + uuid[:13] + "-container1",
			UUID: uuid,
		}); err != nil {
			t.Fatalf("Cannot define new fake domain: %v", err)
		}
	}
	if _, err := ct.domainConn.DefineDomain(&libvirtxml.Domain{
		Name: "other-than-virtlet-domain",
		UUID: "12fdc902-3345-4d8e-a3f1-11a091e59455",
	}); err != nil {
		t.Fatalf("Cannot define new fake domain: %v", err)
	}

	if domains, _ := ct.domainConn.ListDomains(); len(domains) != 4 {
		t.Errorf("Defined 4 domains in fake libvirt but ListDomains() returned %d of them", len(domains))
	}

	// this should remove all domains (including other than virlet defined)
	// with exception of last listed in randomUUIDs slice
	errors := ct.virtTool.removeOrphanDomains(randomUUIDs[2:])
	if errors != nil {
		t.Errorf("removeOrphanDomains returned errors: %v", errors)
	}

	if domains, _ := ct.domainConn.ListDomains(); len(domains) != 1 {
		t.Errorf("After calling removeOrphanDomains expected single remaining domain, ListDomains() returned %d of them", len(domains))
	}

	gm.Verify(t, ct.rec.Content())
}

func TestRootVolumesCleanup(t *testing.T) {
	ct := newContainerTester(t, fake.NewToplevelRecorder())
	defer ct.teardown()

	pool, err := ct.storageConn.LookupStoragePoolByName("volumes")
	if err != nil {
		t.Fatalf("LookupStoragePoolByName did not find 'volumes': %v", err)
	}

	for _, uuid := range randomUUIDs {
		if _, err := pool.CreateStorageVol(&libvirtxml.StorageVolume{
			Name:   "root for " + uuid,
			Target: &libvirtxml.StorageVolumeTarget{Path: "/some/path/virtlet_root_" + uuid},
		}); err != nil {
			t.Fatalf("Cannot define new fake volume: %v", err)
		}
	}
	if _, err := pool.CreateStorageVol(&libvirtxml.StorageVolume{
		Name:   "some other volume",
		Target: &libvirtxml.StorageVolumeTarget{Path: "/path/with/different/prefix"},
	}); err != nil {
		t.Fatalf("Cannot define new fake volume: %v", err)
	}

	if volumes, _ := pool.ListAllVolumes(); len(volumes) != 4 {
		t.Errorf("Defined 4 fake volumes but ListAllVolumes() returned %d of them", len(volumes))
	}

	// this should remove only root volumes for two first elements of randomUUIDs slice
	// keeping intact other
	errors := ct.virtTool.removeOrphanRootVolumes(randomUUIDs[2:])
	if errors != nil {
		t.Errorf("removeOrphanRootVolumes returned errors: %v", errors)
	}

	if volumes, _ := pool.ListAllVolumes(); len(volumes) != 2 {
		t.Errorf("After calling removeOrphanRootVolumes expected two remaining volumes, ListAllVolumes() returned %d of them", len(volumes))
	}

	gm.Verify(t, ct.rec.Content())
}

func TestQcow2VolumesCleanup(t *testing.T) {
	ct := newContainerTester(t, fake.NewToplevelRecorder())
	defer ct.teardown()

	pool, err := ct.storageConn.LookupStoragePoolByName("volumes")
	if err != nil {
		t.Fatalf("LookupStoragePoolByName did not find 'volumes': %v", err)
	}

	for _, uuid := range randomUUIDs {
		if _, err := pool.CreateStorageVol(&libvirtxml.StorageVolume{
			Name:   "qcow flexvolume for " + uuid,
			Target: &libvirtxml.StorageVolumeTarget{Path: "/some/path/virtlet-" + uuid},
		}); err != nil {
			t.Fatalf("Cannot define new fake volume: %v", err)
		}
	}
	if _, err := pool.CreateStorageVol(&libvirtxml.StorageVolume{
		Name:   "some other volume",
		Target: &libvirtxml.StorageVolumeTarget{Path: "/path/with/different/prefix"},
	}); err != nil {
		t.Fatalf("Cannot define new fake volume: %v", err)
	}

	if volumes, _ := pool.ListAllVolumes(); len(volumes) != 4 {
		t.Errorf("Defined 4 fake volumes but ListAllVolumes() returned %d of them", len(volumes))
	}

	// this should remove only qcow2 volumes for two first elements of randomUUIDs slice
	// keeping intact other
	errors := ct.virtTool.removeOrphanQcow2Volumes(randomUUIDs[2:])
	if errors != nil {
		t.Errorf("removeOrphanRootVolumes returned errors: %v", errors)
	}

	if volumes, _ := pool.ListAllVolumes(); len(volumes) != 2 {
		t.Errorf("After calling removeOrphanQcow2Volumes expected two remaining volumes, ListAllVolumes() returned %d of them", len(volumes))
	}

	gm.Verify(t, ct.rec.Content())
}
