/*
Copyright 2018 ZTE corporation

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
	"fmt"
	"os"
	"path"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/utils"
)

// rootVolume denotes the root disk of the VM
type filesystemVolume struct {
	volumeBase
	mount            types.VMMount
	volumeMountPoint string
}

var _ VMVolume = &filesystemVolume{}

var mounter = utils.NewMounter()

func (v *filesystemVolume) UUID() string { return "" }

func (v *filesystemVolume) Setup() (*libvirtxml.DomainDisk, *libvirtxml.DomainFilesystem, error) {
	err := os.MkdirAll(v.volumeMountPoint, 0777)
	if err == nil {
		err = ChownForEmulator(v.volumeMountPoint, true)
	}
	if err == nil {
		err = mounter.Mount(v.mount.HostPath, v.volumeMountPoint, "bind")
	}
	if err == nil {
		err = ChownForEmulator(v.volumeMountPoint, true)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create vm pod path: %v", err)
	}

	fsDef := &libvirtxml.DomainFilesystem{
		AccessMode: "squash",
		Source:     &libvirtxml.DomainFilesystemSource{Mount: &libvirtxml.DomainFilesystemSourceMount{Dir: v.volumeMountPoint}},
		Target:     &libvirtxml.DomainFilesystemTarget{Dir: path.Base(v.mount.ContainerPath)},
	}

	return nil, fsDef, nil
}

func (v *filesystemVolume) Teardown() error {
	var err error
	if _, err = os.Stat(v.volumeMountPoint); err == nil {
		err = mounter.Unmount(v.volumeMountPoint)
	}
	if err == nil {
		err = os.RemoveAll(v.volumeMountPoint)
	}
	if err != nil {
		return fmt.Errorf("failed to tear down fs volume mountpoint '%s': %v", v.volumeMountPoint, err)
	}
	return nil
}
