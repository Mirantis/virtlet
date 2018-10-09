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
	"encoding/hex"
	"fmt"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
	digest "github.com/opencontainers/go-digest"

	"github.com/Mirantis/virtlet/pkg/blockdev"
	"github.com/Mirantis/virtlet/pkg/diskimage"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

// persistentRootVolume represents a root volume that can survive the
// deletion of its pod
type persistentRootVolume struct {
	volumeBase
	dev types.VMVolumeDevice
}

var _ VMVolume = &persistentRootVolume{}

func (v *persistentRootVolume) devHandler() *blockdev.LogicalDeviceHandler {
	return blockdev.NewLogicalDeviceHandler(v.owner.Commander(), "", "")
}

func (v *persistentRootVolume) UUID() string {
	return v.dev.UUID()
}

func (v *persistentRootVolume) dmName() string {
	return "virtlet-dm-" + v.config.DomainUUID
}

func (v *persistentRootVolume) dmPath() string {
	return "/dev/mapper/" + v.dmName()
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
	ldh := v.devHandler()
	headerMatches, err := ldh.EnsureDevHeaderMatches(v.dev.HostPath, hash)

	if err == nil {
		err = ldh.Map(v.dev.HostPath, v.dmName(), imageSize)
	}

	if err == nil && !headerMatches {
		err = v.copyImageToDev(imagePath)
	}

	if err != nil {
		return nil, nil, err
	}

	if err := diskimage.Put(v.dmPath(), v.config.ParsedAnnotations.FilesForRootfs); err != nil {
		return nil, nil, fmt.Errorf("error tuning rootfs with files from configmap: %v", err)
	}

	return &libvirtxml.DomainDisk{
		Device: "disk",
		Source: &libvirtxml.DomainDiskSource{Block: &libvirtxml.DomainDiskSourceBlock{Dev: v.dmPath()}},
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "raw"},
	}, nil, nil
}

func (v *persistentRootVolume) Teardown() error {
	return v.devHandler().Unmap(v.dmName())
}
