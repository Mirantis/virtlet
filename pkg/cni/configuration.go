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

package cni

import (
	"fmt"
	"sort"

	"github.com/containernetworking/cni/libcni"
)

func ReadConfiguration(configsDir string) (*libcni.NetworkConfig, error) {
	confFileNames, err := libcni.ConfFiles(configsDir)
	if err != nil {
		return nil, err
	}

	if confFileNames == nil {
		return nil, fmt.Errorf("can not find any CNI configuration in directory: %s", configsDir)
	}

	sort.Strings(confFileNames)

	return libcni.ConfFromFile(confFileNames[0])
}
