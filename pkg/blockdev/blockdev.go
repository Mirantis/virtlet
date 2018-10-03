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

package blockdev

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/golang/glog"
)

const (
	// VirtletLogicalDevicePrefix denotes the required prefix for
	// the virtual block devices created by Virtlet.
	VirtletLogicalDevicePrefix   = "virtlet-dm-"
	virtletRootfsMagic           = 0x263dbe52ba576702
	virtletRootfsMetadataVersion = 1
	sectorSize                   = 512
	devnameUeventVar             = "DEVNAME="
)

type virtletRootfsHeader struct {
	Magic           uint64
	MetadataVersion uint16
	ImageHash       [sha256.Size]byte
}

// LogicalDeviceHandler makes it possible to store metadata in the
// first sector of a block device, making the rest of the device
// available as another logical device managed by the device mapper.
type LogicalDeviceHandler struct {
	commander utils.Commander
	devPath   string
	sysfsPath string
}

// NewLogicalDeviceHandler creates a new LogicalDeviceHandler using
// the specified commander and paths that should be used in place of
// /dev and /sys directories (empty string to use /dev and /sys,
// respectively)
func NewLogicalDeviceHandler(commander utils.Commander, devPath, sysfsPath string) *LogicalDeviceHandler {
	if devPath == "" {
		devPath = "/dev"
	}
	if sysfsPath == "" {
		sysfsPath = "/sys"
	}
	return &LogicalDeviceHandler{commander, devPath, sysfsPath}
}

// EnsureDevHeaderMatches returns true if the specified block device
// has proper Virtlet header that matches the specified image hash
func (ldh *LogicalDeviceHandler) EnsureDevHeaderMatches(devPath string, imageHash [sha256.Size]byte) (bool, error) {
	f, err := os.OpenFile(devPath, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return false, fmt.Errorf("open %q: %v", devPath, err)
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	var hdr virtletRootfsHeader
	if err := binary.Read(f, binary.BigEndian, &hdr); err != nil {
		return false, fmt.Errorf("reading rootfs header: %v", err)
	}

	headerMatch := true
	switch {
	case hdr.Magic != virtletRootfsMagic || hdr.ImageHash != imageHash:
		headerMatch = false
		if _, err := f.Seek(0, os.SEEK_SET); err != nil {
			return false, fmt.Errorf("seek: %v", err)
		}
		if err := binary.Write(f, binary.BigEndian, virtletRootfsHeader{
			Magic:           virtletRootfsMagic,
			MetadataVersion: virtletRootfsMetadataVersion,
			ImageHash:       imageHash,
		}); err != nil {
			return false, fmt.Errorf("writing rootfs header: %v", err)
		}
	case hdr.MetadataVersion != virtletRootfsMetadataVersion:
		// NOTE: we should handle earlier metadata versions
		// after we introduce new ones. But we can't handle
		// future metadata versions and any non-matching
		// metadata versions are future ones currently, so we
		// don't want to lose any data here.
		return false, fmt.Errorf("unsupported virtlet root device metadata version %v", hdr.MetadataVersion)
	}

	if err := f.Close(); err != nil {
		return false, fmt.Errorf("error closing rootfs device: %v", err)
	}
	f = nil
	return headerMatch, nil
}

// blockDevSizeInSectors returns the size of the block device in sectors
func (ldh *LogicalDeviceHandler) blockDevSizeInSectors(devPath string) (uint64, error) {
	// NOTE: this is also doable via ioctl but this way it's
	// shorter (no need for fake non-linux impl, extra interface,
	// extra fake impl for it). Some links that may help if we
	// decide to use the ioctl later on:
	// https://github.com/karelzak/util-linux/blob/master/disk-utils/blockdev.c
	// https://github.com/aicodix/smr/blob/24aa589f378827a69a07d220f114c169693dacec/smr.go#L29
	out, err := ldh.commander.Command("blockdev", "--getsz", devPath).Run(nil)
	if err != nil {
		return 0, err
	}
	nSectors, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad size value returned by blockdev: %q: %v", out, err)
	}
	return nSectors, nil
}

// Map maps the device sectors starting from 1 to a new virtual block
// device. dmName specifies the name of the new device.
func (ldh *LogicalDeviceHandler) Map(devPath, dmName string, imageSize uint64) error {
	if !strings.HasPrefix(dmName, VirtletLogicalDevicePrefix) {
		return fmt.Errorf("bad logical device name %q: must have prefix %q", dmName, VirtletLogicalDevicePrefix)
	}

	nSectors, err := ldh.blockDevSizeInSectors(devPath)
	if err != nil {
		return err
	}

	// sector 0 is reserved for the Virtlet metadata
	minSectors := (imageSize+sectorSize-1)/sectorSize + 1
	if nSectors < minSectors {
		return fmt.Errorf("block device too small for the image: need at least %d bytes (%d sectors) but got %d bytes (%d sectors)",
			minSectors*sectorSize,
			minSectors,
			nSectors*sectorSize,
			nSectors)
	}

	hostPath, err := filepath.EvalSymlinks(devPath)
	if err != nil {
		return err
	}

	dmTable := fmt.Sprintf("0 %d linear %s 1\n", nSectors-1, hostPath)
	_, err = ldh.commander.Command("dmsetup", "create", dmName).Run([]byte(dmTable))
	return err
}

// Unmap unmaps the virtual block device
func (ldh *LogicalDeviceHandler) Unmap(dmName string) error {
	_, err := ldh.commander.Command("dmsetup", "remove", dmName).Run(nil)
	return err
}

// ListVirtletLogicalDevices returns a list of logical devices managed
// by Virtlet
func (ldh *LogicalDeviceHandler) ListVirtletLogicalDevices() ([]string, error) {
	table, err := ldh.commander.Command("dmsetup", "table").Run(nil)
	if err != nil {
		return nil, fmt.Errorf("dmsetup table: %v", err)
	}
	var r []string
	for _, l := range strings.Split(string(table), "\n") {
		if l == "" {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) != 6 || fields[3] != "linear" {
			continue
		}
		virtDevName := fields[0]
		if strings.HasSuffix(virtDevName, ":") {
			virtDevName = virtDevName[:len(virtDevName)-1]
		}

		devID := fields[4]
		ueventFile := filepath.Join(ldh.sysfsPath, "dev/block", devID, "uevent")
		ueventContent, err := ioutil.ReadFile(ueventFile)
		if err != nil {
			glog.Warningf("error reading %q: %v", ueventFile, err)
			continue
		}
		devName := ""
		for _, ul := range strings.Split(string(ueventContent), "\n") {
			ul = strings.TrimSpace(ul)
			if strings.HasPrefix(ul, devnameUeventVar) {
				devName = ul[len(devnameUeventVar):]
				break
			}
		}
		if devName == "" {
			glog.Warningf("bad uevent file %q: no DEVNAME", ueventFile)
			continue
		}

		isVbd, err := ldh.deviceHasVirtletHeader(devName)
		if err != nil {
			glog.Warningf("checking device file %q: %v", devName, err)
			continue
		}

		if isVbd {
			r = append(r, virtDevName)
		}
	}

	return r, nil
}

func (ldh *LogicalDeviceHandler) deviceHasVirtletHeader(devName string) (bool, error) {
	f, err := os.Open(filepath.Join(ldh.devPath, devName))
	if err != nil {
		return false, err
	}
	defer f.Close()

	var hdr virtletRootfsHeader
	if err := binary.Read(f, binary.BigEndian, &hdr); err != nil {
		return false, err
	}

	return hdr.Magic == virtletRootfsMagic, nil
}
