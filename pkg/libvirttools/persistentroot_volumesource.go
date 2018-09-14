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
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
	digest "github.com/opencontainers/go-digest"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

const (
	virtletRootfsMagic           = 0x263dbe52ba576702
	virtletRootfsMetadataVersion = 1
	sectorSize                   = 512
)

type virtletRootfsHeader struct {
	Magic           uint64
	MetadataVersion uint16
	ImageHash       [sha256.Size]byte
}

// persistentRootVolume represents a root volume that can survive the
// deletion of its pod
type persistentRootVolume struct {
	volumeBase
	dev types.VMVolumeDevice
}

var _ VMVolume = &persistentRootVolume{}

func (v *persistentRootVolume) UUID() string {
	return v.dev.UUID()
}

func (v *persistentRootVolume) dmName() string {
	return "virtlet-dm-" + v.config.PodSandboxID
}

func (v *persistentRootVolume) dmPath() string {
	return "/dev/mapper/" + v.dmName()
}

func (v *persistentRootVolume) ensureDevHeaderMatches(imageHash [sha256.Size]byte) (bool, error) {
	f, err := os.OpenFile(v.dev.HostPath, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return false, fmt.Errorf("open %q: %v", v.dev.HostPath, err)
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

func (v *persistentRootVolume) blockDevSizeInSectors() (uint64, error) {
	// NOTE: this is also doable via ioctl but this way it's
	// shorter (no need for fake non-linux impl, extra interface,
	// extra fake impl for it). Some links that may help if we
	// decide to use the ioctl later on:
	// https://github.com/karelzak/util-linux/blob/master/disk-utils/blockdev.c
	// https://github.com/aicodix/smr/blob/24aa589f378827a69a07d220f114c169693dacec/smr.go#L29
	out, err := v.owner.Commander().Command("blockdev", "--getsz", v.dev.HostPath).Run(nil)
	if err != nil {
		return 0, err
	}
	nSectors, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad size value returned by blockdev: %q: %v", out, err)
	}
	return nSectors, nil
}

func (v *persistentRootVolume) dmCmd(cmd []string, stdin string) error {
	dmCmd := v.owner.Commander().Command(cmd[0], cmd[1:]...)
	var stdinBytes []byte
	if stdin != "" {
		stdinBytes = []byte(stdin)
	}
	_, err := dmCmd.Run(stdinBytes)
	return err
}

func (v *persistentRootVolume) dmSetup(imageSize uint64) error {
	nSectors, err := v.blockDevSizeInSectors()
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
	hostPath, err := filepath.EvalSymlinks(v.dev.HostPath)
	if err != nil {
		return err
	}
	dmTable := fmt.Sprintf("0 %d linear %s 1\n", nSectors-1, hostPath)
	dmCmd := v.owner.Commander().Command("dmsetup", "create", v.dmName())
	_, err = dmCmd.Run([]byte(dmTable))
	return err
}

func (v *persistentRootVolume) copyImageToDev(imagePath string) error {
	_, err := v.owner.Commander().Command("qemu-img", "convert", "-O", "raw", imagePath, v.dmPath()).Run(nil)
	return err
}

func (v *persistentRootVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	imagePath, imageDigest, imageSize, err := v.owner.ImageManager().GetImagePathDigestAndVirtualSize(v.config.Image)
	if err != nil {
		return nil, nil, err
	}

	if imageDigest.Algorithm() != digest.SHA256 {
		return nil, nil, fmt.Errorf("unsupported digest algorithm %q", imageDigest.Algorithm())
	}
	imageHash, err := hex.DecodeString(imageDigest.Hex())
	if err != nil {
		return nil, nil, fmt.Errorf("bad digest hex: %q", imageDigest.Hex())
	}
	if len(imageHash) != sha256.Size {
		return nil, nil, fmt.Errorf("bad digest size: %q", imageDigest.Hex())
	}

	var hash [sha256.Size]byte
	copy(hash[:], imageHash)
	headerMatches, err := v.ensureDevHeaderMatches(hash)

	if err == nil {
		err = v.dmSetup(imageSize)
	}

	if err == nil && !headerMatches {
		err = v.copyImageToDev(imagePath)
	}

	if err != nil {
		return nil, nil, err
	}

	return &libvirtxml.DomainDisk{
		Device: "disk",
		Source: &libvirtxml.DomainDiskSource{Block: &libvirtxml.DomainDiskSourceBlock{Dev: v.dmPath()}}, //hostPath}},
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
	}, nil, nil
}

func (v *persistentRootVolume) Teardown() error {
	_, err := v.owner.Commander().Command("dmsetup", "remove", v.dmName()).Run(nil)
	return err
}
