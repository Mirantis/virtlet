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

package version

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"runtime"

	"github.com/ghodss/yaml"
)

// Info specifies Virtlet version info
type Info struct {
	// NodeName denotes the name of the node this info belongs too
	// (empty if not applicable)
	NodeName string `json:",omitempty"`
	// Major is the major version number
	Major string `json:"major"`
	// Minor is the minor version number
	Minor string `json:"minor"`
	// GitVersion is the full version string
	GitVersion string `json:"gitVersion"`
	// GitCommit is the git commit id
	GitCommit string `json:"gitCommit"`
	// GitTreeState is the git tree state, which can be either "clean" or "dirty"
	GitTreeState string `json:"gitTreeState"`
	// BuildDate is the build date, e.g. 2018-04-16T18:48:12Z
	BuildDate string `json:"buildDate"`
	// GoVersion is the Go version that was used to build Virtlet
	GoVersion string `json:"goVersion"`
	// Compiler is the name of the compiler toolchain that
	// built the binary (either "gc" or "gccgo")
	Compiler string `json:"compiler"`
	// Platform denotes the platform such as "linux" or "darwin"
	Platform string `json:"platform"`
	// ImageTag specifies the image tag to use for Virtelt
	ImageTag string `json:"imageTag"`
}

type formatVersion func(v Info) []byte

func (v Info) text(indent string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%sVersion:    %s\n", indent, v.GitVersion)
	fmt.Fprintf(&b, "%sCommit:     %s\n", indent, v.GitCommit)
	fmt.Fprintf(&b, "%sBuild Date: %s\n", indent, v.BuildDate)
	fmt.Fprintf(&b, "%sGo Version: %s\n", indent, v.GoVersion)
	fmt.Fprintf(&b, "%sCompiler:   %s\n", indent, v.Compiler)
	fmt.Fprintf(&b, "%sPlatform:   %s\n", indent, v.Platform)
	if v.ImageTag != "" {
		fmt.Fprintf(&b, "%sImageTag:   %s\n", indent, v.ImageTag)
	}
	return b.Bytes()
}

var versionFormats = map[string]formatVersion{
	"text": func(v Info) []byte {
		return v.text("")
	},
	"short": func(v Info) []byte {
		return []byte(v.GitVersion)
	},
	"json": func(v Info) []byte {
		out, err := json.Marshal(v)
		if err != nil {
			log.Panicf("Error marshaling version info to JSON: %#v: %v", v, err)
		}
		return out
	},
	"yaml": func(v Info) []byte {
		out, err := yaml.Marshal(v)
		if err != nil {
			log.Panicf("Error marshaling version info to YAML: %#v: %v", v, err)
		}
		return out
	},
}

// ToBytes returns a text representation of Info using the specified
// format which can be one of "text", "short", "json" or "yaml"
func (v Info) ToBytes(format string) ([]byte, error) {
	f := versionFormats[format]
	if f == nil {
		return nil, fmt.Errorf("bad version format %q", format)
	}
	return f(v), nil
}

// Get returns the codebase version.
func Get() Info {
	return Info{
		Major:        gitMajor,
		Minor:        gitMinor,
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		ImageTag:     imageTag,
	}
}

// ClusterVersionInfo specifies Virtlet version info for the whole
// cluster and the client
type ClusterVersionInfo struct {
	// ClientVersion denotes the version of Virtlet command line tool
	ClientVersion Info `json:"clientVersion"`
	// NodeVersions specify versions for each node that runs Virtlet
	NodeVersions []Info `json:"nodeVersions,omitempty"`
}

type formatClusterVersion func(v ClusterVersionInfo) []byte

var clusterVersionFormats = map[string]formatClusterVersion{
	"text": func(v ClusterVersionInfo) []byte {
		var b bytes.Buffer
		fmt.Fprintf(&b, "Client:\n%s", v.ClientVersion.text("  "))
		for _, nv := range v.NodeVersions {
			fmt.Fprintf(&b, "Node %s:\n%s", nv.NodeName, nv.text("  "))
		}
		return b.Bytes()
	},
	"short": func(v ClusterVersionInfo) []byte {
		var b bytes.Buffer
		fmt.Fprintf(&b, "Client: %s\n", v.ClientVersion.GitVersion)
		for _, nv := range v.NodeVersions {
			fmt.Fprintf(&b, "Node %s: %s\n", nv.NodeName, nv.GitVersion)
		}
		return b.Bytes()
	},
	"json": func(v ClusterVersionInfo) []byte {
		out, err := json.Marshal(v)
		if err != nil {
			log.Panicf("Error marshaling version info to JSON: %#v: %v", v, err)
		}
		return out
	},
	"yaml": func(v ClusterVersionInfo) []byte {
		out, err := yaml.Marshal(v)
		if err != nil {
			log.Panicf("Error marshaling version info to YAML: %#v: %v", v, err)
		}
		return out
	},
}

// ToBytes returns a text representation of ClusterVersionInfo using
// the specified format which can be one of "text", "short", "json" or
// "yaml"
func (v ClusterVersionInfo) ToBytes(format string) ([]byte, error) {
	f := clusterVersionFormats[format]
	if f == nil {
		return nil, fmt.Errorf("bad version format %q", format)
	}
	return f(v), nil
}

// AreNodesConsistent returns true if all the nodes that run Virtlet
// run exactly same Virtlet build
func (v ClusterVersionInfo) AreNodesConsistent() bool {
	if len(v.NodeVersions) == 0 {
		return true
	}
	nv := v.NodeVersions[0]
	nv.NodeName = ""
	for _, curNV := range v.NodeVersions[1:] {
		curNV.NodeName = ""
		if nv != curNV {
			return false
		}
	}
	return true
}
