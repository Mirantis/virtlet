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
	"crypto/sha256"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	blockdev "github.com/Mirantis/virtlet/pkg/blockdev"
	fakeblockdev "github.com/Mirantis/virtlet/pkg/blockdev/fake"
	fakeutils "github.com/Mirantis/virtlet/pkg/utils/fake"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/tests/gm"
)

var (
	testUUIDs = [...]string{
		"5edfe2ad-9852-439b-bbfb-3fe8b7c72906",
		"8a6163c3-e4ee-488f-836a-d2abe92d0744",
		"13f51f8d-0f4e-4538-9db0-413380ff9c84",
	}
)

func TestDomainCleanup(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil)
	defer ct.teardown()

	for _, uuid := range testUUIDs {
		if _, err := ct.domainConn.DefineDomain(&libvirtxml.Domain{
			Name: "virtlet-" + uuid[:13] + "-container1",
			UUID: uuid,
		}); err != nil {
			t.Fatalf("Cannot define the fake domain: %v", err)
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
	// with an exception of the last listed in testUUIDs slice
	if errors := ct.virtTool.removeOrphanDomains(testUUIDs[2:]); len(errors) != 0 {
		t.Errorf("removeOrphanDomains returned errors: %v", errors)
	}

	if domains, _ := ct.domainConn.ListDomains(); len(domains) != 1 {
		t.Errorf("Expected a single remaining domain, ListDomains() returned %d of them", len(domains))
	}

	gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
}

func TestRootVolumesCleanup(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil)
	defer ct.teardown()

	pool, err := ct.virtTool.StoragePool()
	if err != nil {
		t.Fatalf("StoragePool(): %v", err)
	}

	for _, uuid := range testUUIDs {
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

	if volumes, _ := pool.ListVolumes(); len(volumes) != 4 {
		t.Errorf("Defined 4 fake volumes but ListVolumes() returned %d of them", len(volumes))
	}

	// this should remove only root volumes corresponding to the two first
	// elements of testUUIDs slice, keeping others
	if errors := ct.virtTool.removeOrphanRootVolumes(testUUIDs[2:]); len(errors) != 0 {
		t.Errorf("removeOrphanRootVolumes returned errors: %v", errors)
	}

	if volumes, _ := pool.ListVolumes(); len(volumes) != 2 {
		t.Errorf("Expected 2 volumes to remain, but ListVolumes() returned %d of them", len(volumes))
	}

	gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
}

func TestQcow2VolumesCleanup(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil)
	defer ct.teardown()

	pool, err := ct.virtTool.StoragePool()
	if err != nil {
		t.Fatalf("StoragePool(): %v", err)
	}

	for _, uuid := range testUUIDs {
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

	if volumes, _ := pool.ListVolumes(); len(volumes) != 4 {
		t.Errorf("Defined 4 fake volumes but ListVolumes() returned %d of them", len(volumes))
	}

	// this should remove only ephemeral qcow2 volumes corresponding to
	// the two first elements of testUUIDs slice, keeping others
	if errors := ct.virtTool.removeOrphanQcow2Volumes(testUUIDs[2:]); len(errors) != 0 {
		t.Errorf("removeOrphanRootVolumes returned errors: %v", errors)
	}

	if volumes, _ := pool.ListVolumes(); len(volumes) != 2 {
		t.Errorf("Expected two remaining volumes, ListVolumes() returned %d of them", len(volumes))
	}

	gm.Verify(t, gm.NewYamlVerifier(ct.rec.Content()))
}

func TestConfigISOsCleanup(t *testing.T) {
	ct := newContainerTester(t, testutils.NewToplevelRecorder(), nil)
	defer ct.teardown()

	directory, err := ioutil.TempDir("", "virtlet-tests-")
	if err != nil {
		t.Fatalf("TempDir() returned: %v", err)
	}
	defer os.RemoveAll(directory)

	for _, uuid := range testUUIDs {
		fname := filepath.Join(directory, "config-"+uuid+".iso")
		if file, err := os.Create(fname); err != nil {
			t.Fatalf("Cannot create fake iso with name %q: %v", fname, err)
		} else {
			file.Close()
		}
	}
	fname := filepath.Join(directory, "some other.iso")
	if file, err := os.Create(fname); err != nil {
		t.Fatalf("Cannot create fake iso with name %q: %v", fname, err)
	} else {
		file.Close()
	}

	preCallFileNames, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil {
		t.Fatalf("Error globbing names in temporary directory: %v", err)
	}
	if len(preCallFileNames) != 4 {
		t.Fatalf("Expected 4 files in temporary directory, found: %d", len(preCallFileNames))
	}

	// this should remove only config iso file corresponding to the first
	// element of testUUIDs slice, keeping other files
	if errors := ct.virtTool.removeOrphanConfigImages(testUUIDs[1:], directory); len(errors) != 0 {
		t.Errorf("removeOrphanConfigImages returned errors: %v", errors)
	}

	postCallFileNames, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil {
		t.Fatalf("Error globbing names in the temporary directory: %v", err)
	}

	diff := difference(preCallFileNames, postCallFileNames)
	if len(diff) != 1 {
		t.Fatalf("Expected removeOrphanConfigImages to remove single file, but it removed %d files", len(diff))
	}

	expectedPath := filepath.Join(directory, "config-"+testUUIDs[0]+".iso")
	if diff[0] != expectedPath {
		t.Fatalf("Expected removeOrphanConfigImages to remove only %q file, but it also removed: %q", expectedPath, diff[0])
	}

	// no gm validation, because we are testing only file operations in this test
}

func TestDeviceMapperCleanup(t *testing.T) {
	fakeblockdev.WithFakeRootDevsAndSysfs(t, func(devPaths []string, table, devDir, sysfsDir string) {
		dmRemoveCmd := "dmsetup remove virtlet-dm-9a322047-1f0d-4395-8e43-6e1b310ce6f3"
		ct := newContainerTester(t, testutils.NewToplevelRecorder(), []fakeutils.CmdSpec{
			{
				Match:  "^dmsetup table$",
				Stdout: table,
			},
			{
				Match: "^" + dmRemoveCmd + "$",
			},
		})
		defer ct.teardown()

		ldh := blockdev.NewLogicalDeviceHandler(ct.virtTool.commander, devDir, sysfsDir)
		for _, devPath := range devPaths {
			if _, err := ldh.EnsureDevHeaderMatches(devPath, sha256.Sum256([]byte("foobar"))); err != nil {
				t.Fatalf("EnsureDevHeaderMatches(): %v", err)
			}
		}

		for _, uuid := range testUUIDs {
			if _, err := ct.domainConn.DefineDomain(&libvirtxml.Domain{
				Name: "virtlet-" + uuid[:13] + "-container1",
				UUID: uuid,
			}); err != nil {
				t.Fatalf("Cannot define the fake domain: %v", err)
			}
		}

		if errors := ct.virtTool.removeOrphanVirtualBlockDevices(testUUIDs[:], devDir, sysfsDir); len(errors) != 0 {
			t.Errorf("removeOrphanDomains returned errors: %v", errors)
		}

		n := 0
		for _, r := range ct.rec.Content() {
			if r.Name == "CMD" && r.Value.(map[string]string)["cmd"] == dmRemoveCmd {
				n++
			}
		}
		if n != 1 {
			t.Errorf("dmsetup remove for the orphaned volume is expected to be called exactly 1 time, but was called %d times", n)
		}
	})
	// no gm validation b/c we just verify 'dmsetup remove' command above
}

// https://stackoverflow.com/a/45428032
// difference returns the elements in a that aren't in b
func difference(a, b []string) []string {
	mb := map[string]bool{}
	for _, x := range b {
		mb[x] = true
	}
	ab := []string{}
	for _, x := range a {
		if _, ok := mb[x]; !ok {
			ab = append(ab, x)
		}
	}
	return ab
}
