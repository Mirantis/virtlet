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

	"github.com/golang/glog"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	digest "github.com/opencontainers/go-digest"
	"golang.org/x/sys/unix"

	"github.com/Mirantis/virtlet/pkg/blockdev"
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

func (v *persistentRootVolume) IsDisk() bool { return true }

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
	syncFiles(imagePath, v.dev.HostPath, v.dmPath())
	if _, err := v.owner.Commander().Command("qemu-img", "convert", "-O", "raw", imagePath, v.dmPath()).Run(nil); err != nil {
		return err
	}
	syncFiles(v.dmPath())
	return nil
}

func (v *persistentRootVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	glog.V(4).Infof("Persistent rootfs setup on %q", v.dev.HostPath)
	imagePath, imageDigest, imageSize, err := v.owner.ImageManager().GetImagePathDigestAndVirtualSize(v.config.Image)
	if err != nil {
		glog.V(4).Infof("Persistent rootfs setup on %q: image info error: %v", v.dev.HostPath, err)
		return nil, nil, err
	}

	if imageDigest.Algorithm() != digest.SHA256 {
		glog.V(4).Infof("Persistent rootfs setup on %q: image info error: %v", v.dev.HostPath, err)
		return nil, nil, fmt.Errorf("unsupported digest algorithm %q", imageDigest.Algorithm())
	}
	imageHash, err := hex.DecodeString(imageDigest.Hex())
	if err != nil {
		glog.V(4).Infof("Persistent rootfs setup on %q: bad digest hex: %q", v.dev.HostPath, imageDigest.Hex())
		return nil, nil, fmt.Errorf("bad digest hex: %q", imageDigest.Hex())
	}
	if len(imageHash) != sha256.Size {
		glog.V(4).Infof("Persistent rootfs setup on %q: bad digest size: %q", v.dev.HostPath, imageDigest.Hex())
		return nil, nil, fmt.Errorf("bad digest size: %q", imageDigest.Hex())
	}

	var hash [sha256.Size]byte
	copy(hash[:], imageHash)
	ldh := v.devHandler()
	headerMatches, err := ldh.EnsureDevHeaderMatches(v.dev.HostPath, hash)

	if err == nil {
		glog.V(4).Infof("Persistent rootfs setup on %q: headerMatches: %v", v.dev.HostPath, headerMatches)
		err = ldh.Map(v.dev.HostPath, v.dmName(), imageSize)
	}

	if err == nil {
		if headerMatches {
			glog.V(4).Infof("Persistent rootfs setup on %q: header matches image %q, not overwriting", v.dev.HostPath, imagePath)
		} else {
			glog.V(4).Infof("Persistent rootfs setup on %q: writing image from %q", v.dev.HostPath, imagePath)
			err = v.copyImageToDev(imagePath)
		}
	}

	if err != nil {
		glog.V(4).Infof("Persistent rootfs setup on %q: error: %v", v.dev.HostPath, err)
		return nil, nil, err
	}

	if len(v.config.ParsedAnnotations.InjectedFiles) > 0 {
		if err := v.owner.StorageConnection().PutFiles(v.dmPath(), v.config.ParsedAnnotations.InjectedFiles); err != nil {
			return nil, nil, fmt.Errorf("error adding files to rootfs: %v", err)
		}
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

func syncFiles(paths ...string) error {
	// https://www.redhat.com/archives/libguestfs/2012-July/msg00009.html
	unix.Sync()
	for _, p := range paths {
		fd, err := unix.Open(p, unix.O_RDWR|unix.O_SYNC, 0)
		if err != nil {
			return err
		}
		if err := unix.Fsync(fd); err != nil {
			unix.Close(fd)
			return err
		}
		if err := unix.Close(fd); err != nil {
			return err
		}
	}
	return nil
}
