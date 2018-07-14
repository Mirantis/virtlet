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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

const (
	flexvolumeSubdir   = "volumes/virtlet~flexvolume_driver"
	flexvolumeDataFile = "virtlet-flexvolume.json"
)

type flexvolumeSource func(volumeName, configPath string, config *types.VMConfig, owner volumeOwner) (VMVolume, error)

var flexvolumeTypeMap = map[string]flexvolumeSource{}

func addFlexvolumeSource(fvType string, source flexvolumeSource) {
	flexvolumeTypeMap[fvType] = source
}

// ScanFlexVolumes using prepared by kubelet volumes and contained in pod sandbox
// annotations prepares volumes to be passed to libvirt as a DomainDisk definitions.
func ScanFlexVolumes(config *types.VMConfig, owner volumeOwner) ([]VMVolume, error) {
	dir := filepath.Join(owner.KubeletRootDir(), config.PodSandboxID, flexvolumeSubdir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		glog.V(2).Infof("No flexvolumes to process for %q with uuid %q", config.Name, config.DomainUUID)
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	glog.V(2).Info("Processing flexvolumes for flexvolume_driver")
	volDirItems, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	glog.V(2).Infof("Found flexvolumes definitions at %s:\n%#v", dir, volDirItems)
	var vols []VMVolume
	for _, fi := range volDirItems {
		if !fi.IsDir() {
			continue
		}
		dataFilePath := filepath.Join(dir, fi.Name(), flexvolumeDataFile)
		content, err := ioutil.ReadFile(dataFilePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("error reading flexvolume config %q: %v", dataFilePath, err)
		}
		var msi map[string]interface{}
		if err = json.Unmarshal(content, &msi); err != nil {
			return nil, fmt.Errorf("error unmarshal flexvolume config %q: %v", dataFilePath, err)
		}
		fvType, _ := msi["type"].(string)
		if fvType == "" {
			return nil, fmt.Errorf("flexvolume config %q: need to specify 'type' (a string)", dataFilePath)
		}
		fvSource, found := flexvolumeTypeMap[fvType]
		if !found {
			return nil, fmt.Errorf("bad flexvolume config %q: bad type %q", dataFilePath, fvType)
		}
		vol, err := fvSource(fi.Name(), dataFilePath, config, owner)
		if err != nil {
			return nil, err
		}
		glog.V(3).Infof("Found flexvolume: %s", string(content))
		vols = append(vols, vol)
	}
	return vols, nil
}
