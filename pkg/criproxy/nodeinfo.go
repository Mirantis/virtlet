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

package criproxy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// NodeInfo represents an information needed by dockershim when it's
// run from bootstrapped CRI proxy
type NodeInfo struct {
	KubeletArgs    []string
	NodeName       string
	DockerEndpoint string
	FirstRun       bool
}

// NodeInfoFromCommandLine constructs a NodeInfo from a command line
// file, formatted like /proc/NNN/cmdline
func NodeInfoFromCommandLine(commandLineFile string) (*NodeInfo, error) {
	bs, err := ioutil.ReadFile(commandLineFile)
	if err != nil {
		return nil, fmt.Errorf("can't read kubelet command line from %q: %v", commandLineFile, err)
	}

	if len(bs) == 0 {
		return &NodeInfo{KubeletArgs: []string{""}}, nil
	}
	if bs[len(bs)-1] == 0 {
		bs = bs[:len(bs)-1]
	}
	return &NodeInfo{KubeletArgs: strings.Split(string(bs), "\x00")}, nil
}

func LoadNodeInfo(filename string) (*NodeInfo, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading node info from %q: %v", filename, err)
	}
	var ni NodeInfo
	if err := json.Unmarshal(data, &ni); err != nil {
		return nil, fmt.Errorf("error parsing node info from %q: %v", filename, err)
	}
	return &ni, nil
}

func (ni *NodeInfo) Write(filename string) error {
	out, err := json.Marshal(ni)
	if err != nil {
		log.Panicf("Error marshalling node info: %v", err)
	}
	destDir := filepath.Dir(filename)
	if err := os.MkdirAll(destDir, 0777); err != nil {
		return fmt.Errorf("can't make criproxy info directory %q: %v", destDir, err)
	}
	if err := ioutil.WriteFile(filename, out, 0777); err != nil {
		return fmt.Errorf("error writing node info to %q: %v", filename, err)
	}
	return nil
}
