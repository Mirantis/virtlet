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
	"strings"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

// GetFileSystemVolumes using prepared by kubelet volumes and contained in pod sandbox
// annotations prepares volumes to be passed to libvirt as a DomainFileSystem definitions.
func GetFileSystemVolumes(config *types.VMConfig, owner volumeOwner) ([]VMVolume, error) {
	volumePoolPath := supportedStoragePools[owner.VolumePoolName()]
	if _, err := os.Stat(volumePoolPath); err != nil {
		return nil, err
	}

	var fsVolumes []VMVolume
	for index, mount := range config.Mounts {
		if isRegularFile(mount.HostPath) ||
			strings.Contains(mount.HostPath, flexvolumeSubdir) ||
			strings.Contains(mount.HostPath, "kubernetes.io~secret") ||
			strings.Contains(mount.HostPath, "kubernetes.io~configmap") {
			continue
		}

		// `Index` is used to avoid causing conflicts as multiple host paths can have the same `path.Base`
		volumeDirName := fmt.Sprintf("virtlet_%s_%s_%d", config.DomainUUID, path.Base(mount.HostPath), index)
		volumeMountPoint := path.Join(volumePoolPath, volumeDirName)
		fsVolumes = append(fsVolumes, &filesystemVolume{mount: mount, volumeMountPoint: volumeMountPoint})
	}

	return fsVolumes, nil
}
