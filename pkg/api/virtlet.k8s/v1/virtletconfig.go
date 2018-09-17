/*
Copyright 2018 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or ≈git-agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VirtletConfig denotes a configuration for VirtletManager.
type VirtletConfig struct {
	// FdServerSocketPath specifies the path to fdServer socket.
	FDServerSocketPath *string `json:"fdServerSocketPath,omitempty"`
	// DatabasePath specifies the path to Virtlet database.
	DatabasePath *string `json:"databasePath,omitempty"`
	// DownloadProtocol specifies the download protocol to use.
	// It defaults to "https".
	DownloadProtocol *string `json:"downloadProtocol,omitempty"`
	// ImageDir specifies the image store directory.
	ImageDir *string `json:"imageDir,omitempty"`
	// ImageTranslationConfigsDir specifies the directory with
	// image translation configuration files. Empty string means
	// such directory is not used.
	ImageTranslationConfigsDir *string `json:"imageTranslationConfigsDir,omitempty"`
	// SkipImageTranslation disables image translations.
	SkipImageTranslation *bool `json:"skipImageTranslation,omitempty"`
	// LibvirtURI specifies the libvirt connnection URI.
	LibvirtURI *string `json:"libvirtURI,omitempty"`
	// RawDevices specifies a comma-separated list of raw device
	// glob patterns which VMs can access.
	RawDevices *string `json:"rawDevices,omitempty"`
	// CRISocketPath specifies the socket path for the gRPC endpoint.
	CRISocketPath *string `json:"criSocketPath,omitempty"`
	// DisableLogging disables the streaming server
	DisableLogging *bool `json:"disableLogging,omitempty"`
	// True if KVM should be disabled.
	DisableKVM *bool `json:"disableKVM,omitempty"`
	// True if SR-IOV support should be enabled.
	EnableSriov *bool `json:"enableSriov,omitempty"`
	// CNIPluginDir specifies the location of CNI configurations.
	CNIPluginDir *string `json:"cniPluginDir,omitempty"`
	// CNIConfigDir specifies the location of CNI configurations.
	CNIConfigDir *string `json:"cniConfigDir,omitempty"`
	// CalicoSubnetSize specifies the size of Calico subnetwork.
	CalicoSubnetSize *int `json:"calicoSubnetSize,omitempty"`
	// EnableRegexpImageTranslation is true if regexp-based image
	// translations are enabled.
	EnableRegexpImageTranslation *bool `json:"enableRegexpImageTranslation,omitempty"`
	// CPUModel specifies the default CPU model to use in the libvirt domain definition.
	// It can be overridden using VirtletCPUModel pod annotation.
	CPUModel *string `json:"cpuModel,omitempty"`
	// StreamPort specifies the configurable stream port of virtlet server.
	StreamPort *int `json:"streamPort,omitempty"`
	// LogLevel specifies the log level to use
	LogLevel *int `json:"logLevel,omitempty"`
	// Kubelet's root dir
	KubeletRootDir *string `json:"kubeletRootDir,omitempty"`
}

// VirtletConfigMappingSpec is the contents of a VirtletConfigMapping.
type VirtletConfigMappingSpec struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`
	// NodeSelector specifies the labels that must be matched for this
	// mapping to apply to the node.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Node name to match.
	NodeName string `json:"nodeName,omitempty"`
	// Priority specifies the priority of this setting.
	Priority int
	// VirtletConfig to apply.
	Config *VirtletConfig `json:"config,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VirtletConfigMapping specifies the mapping of node names or labels
// to Virtlet configs.
type VirtletConfigMapping struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`

	Spec VirtletConfigMappingSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VirtletConfigMappingList lists the mappings between node names or
// labels and Virtlet configs.
type VirtletConfigMappingList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []VirtletConfigMapping `json:"items,omitempty"`
}
