// +build linux

/*
Copyright 2016 Mirantis

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

package diskimage

import (
	"errors"
	"path/filepath"

	"github.com/Mirantis/virtlet/pkg/diskimage/guestfs"
)

// FormatDisk partitions the specified image file by writing an MBR with
// a single partition and then formatting that partition as an ext4 filesystem.
func FormatDisk(path string) error {
	g, err := guestfs.Create()
	if err != nil {
		return err
	}
	defer func() {
		g.Shutdown()
		g.Close()
	}()

	// Set the trace flag so that we can see each libguestfs call.
	g.Set_trace(true)

	// Attach the disk image to libguestfs.
	optargs := guestfs.OptargsAdd_drive{
		Format_is_set:   true,
		Format:          "qcow2",
		Readonly_is_set: true,
		Readonly:        false,
	}

	if gErr := g.Add_drive(path, &optargs); gErr != nil {
		return errors.New(gErr.String())
	}

	// Run the libguestfs back-end.
	if gErr := g.Launch(); gErr != nil {
		return errors.New(gErr.String())
	}

	// Get the list of devices.  Because we only added one drive
	// above, we expect that this list should contain a single
	// element.
	devices, gErr := g.List_devices()
	if gErr != nil {
		return errors.New(gErr.String())
	}
	if len(devices) != 1 {
		return errors.New("expected a single device from list-devices")
	}

	// Partition the disk as one single MBR partition.
	gErr = g.Part_disk(devices[0], "mbr")
	if gErr != nil {
		return errors.New(gErr.String())
	}

	// Get the list of partitions.  We expect a single element, which
	// is the partition we have just created.
	partitions, gErr := g.List_partitions()
	if gErr != nil {
		return errors.New(gErr.String())
	}
	if len(partitions) != 1 {
		return errors.New("expected a single partition from list-partitions")
	}

	// Create a filesystem on the partition.
	gErr = g.Mkfs("ext4", partitions[0], nil)
	if gErr != nil {
		return errors.New(gErr.String())
	}

	return nil
}

// Put insert files to image creating all necessary subdirs
func Put(image string, files map[string][]byte) error {
	g, err := guestfs.Create()
	if err != nil {
		return err
	}
	defer func() {
		g.Shutdown()
		g.Close()
	}()

	// Set the trace flag so that we can see each libguestfs call.
	g.Set_trace(true)

	if gErr := g.Add_drive(image, nil); gErr != nil {
		return errors.New(gErr.String())
	}

	// Run the libguestfs back-end.
	if gErr := g.Launch(); gErr != nil {
		return errors.New(gErr.String())
	}

	// Get the list of devices.  Because we only added one drive
	// above, we expect that this list should contain a single
	// element.
	devices, gErr := g.List_devices()
	if gErr != nil {
		return errors.New(gErr.String())
	}
	if len(devices) != 1 {
		return errors.New("expected a single device from list-devices")
	}

	partitions, gErr := g.List_partitions()
	if gErr != nil {
		return errors.New(gErr.String())
	}
	if len(partitions) < 1 {
		return errors.New("expected a single partition from list-partitions")
	}

	// Try to mount fist partition of image as /.
	if gErr := g.Mount(partitions[0], "/"); gErr != nil {
		return errors.New(gErr.String())
	}

	for path, content := range files {
		dir, _ := filepath.Split(path)
		if len(dir) > 1 {
			// Just in case - try to create directory structure
			// as it can be missing.
			if gErr := g.Mkdir_p(dir); gErr != nil {
				return errors.New(gErr.String())
			}
		}
		if gErr := g.Write(path, content); gErr != nil {
			return errors.New(gErr.String())
		}
	}

	return nil
}
