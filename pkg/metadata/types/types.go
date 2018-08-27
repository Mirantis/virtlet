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

package types

import (
	"github.com/Mirantis/virtlet/pkg/network"
)

// PodSandboxState specifies the state of the sandbox
type PodSandboxState int32

const (
	// PodSandboxState_SANDBOX_READY specifies that the pod is ready.
	PodSandboxState_SANDBOX_READY PodSandboxState = 0
	// PodSandboxState_SANDBOX_READY specifies that the pod is not ready.
	// This includes errors during RunPodSandbox.
	PodSandboxState_SANDBOX_NOTREADY PodSandboxState = 1
)

// Protocol specifies the protocol for a port mapping.
type Protocol int32

const (
	// Protocol_TCP specifies TCP protocol.
	Protocol_TCP Protocol = 0
	// Protocol_TCP specifies UDP protocol.
	Protocol_UDP Protocol = 1
)

// ContainerState specifies the state of a container
type ContainerState int32

const (
	// ContainerState_CONTAINER_CREATED means that the container is just created.
	ContainerState_CONTAINER_CREATED ContainerState = 0
	// ContainerState_CONTAINER_CREATED means that the container is running.
	ContainerState_CONTAINER_RUNNING ContainerState = 1
	// ContainerState_CONTAINER_CREATED means that the container has exited.
	ContainerState_CONTAINER_EXITED ContainerState = 2
	// ContainerState_CONTAINER_CREATED means that the container state is not known.
	ContainerState_CONTAINER_UNKNOWN ContainerState = 3
)

// PodSandboxInfo contains metadata information about pod sandbox instance
type PodSandboxInfo struct {
	// Pod ID.
	PodID string
	// Sandbox configuration information.
	Config *PodSandboxConfig
	// Creation timestamp.
	CreatedAt int64
	// Sandbox state.
	State PodSandboxState
	// Sandbox network state.
	ContainerSideNetwork *network.ContainerSideNetwork
}

// ContainerInfo contains metadata information about container instance
type ContainerInfo struct {
	// Container ID
	Id string
	// Container name
	Name string
	// Container creation timestamp
	CreatedAt int64
	// Container startup timestamp
	StartedAt int64
	// Current state of the container
	State ContainerState
	// Container configuration
	Config VMConfig
}

// NamespaceOption provides options for Linux namespaces.
type NamespaceOption struct {
	// If set, use the host's network namespace.
	HostNetwork bool
	// If set, use the host's PID namespace.
	HostPid bool
	// If set, use the host's IPC namespace.
	HostIpc bool
}

// PodSandboxFilter is used to filter a list of PodSandboxes.
// All those fields are combined with 'AND'
type PodSandboxFilter struct {
	// ID of the sandbox.
	Id string
	// State of the sandbox.
	State *PodSandboxState
	// LabelSelector to select matches.
	// Only api.MatchLabels is supported for now and the requirements
	// are ANDed. MatchExpressions is not supported yet.
	LabelSelector map[string]string
}

// DNSConfig specifies the DNS servers and search domains of a sandbox.
type DNSConfig struct {
	// List of DNS servers of the cluster.
	Servers []string
	// List of DNS search domains of the cluster.
	Searches []string
	// List of DNS options. See https://linux.die.net/man/5/resolv.conf
	// for all available options.
	Options []string
}

// PortMapping specifies the port mapping configurations of a sandbox.
type PortMapping struct {
	// Protocol of the port mapping.
	Protocol Protocol
	// Port number within the container. Default: 0 (not specified).
	ContainerPort int32
	// Port number on the host. Default: 0 (not specified).
	HostPort int32
	// Host IP.
	HostIp string
}

// PodSandboxConfig holds all the required and optional fields for creating a
// sandbox.
type PodSandboxConfig struct {
	// Pod name of the sandbox.
	Name string
	// Pod UID of the sandbox.
	Uid string
	// Pod namespace of the sandbox.
	Namespace string
	// Attempt number of creating the sandbox. Default: 0.
	Attempt uint32
	// Hostname of the sandbox.
	Hostname string
	// Path to the directory on the host in which container log files are
	// stored.
	LogDirectory string
	// DNS config for the sandbox.
	DnsConfig *DNSConfig
	// Port mappings for the sandbox.
	PortMappings []*PortMapping
	// Key-value pairs that may be used to scope and select individual resources.
	Labels map[string]string
	// Unstructured key-value map that may be set by the kubelet to store and
	// retrieve arbitrary metadata. This will include any annotations set on a
	// pod through the Kubernetes API.
	Annotations map[string]string
	// Optional configurations specific to Linux hosts.
	CgroupParent string
}

// VMKeyValue denotes a key-value pair.
type VMKeyValue struct {
	// Key contains the key part of the pair.
	Key string
	// Value contains the value part of the pair.
	Value string
}

// VMMount denotes a host directory corresponding to a volume which is
// to be mounted inside the VM.
type VMMount struct {
	// ContainerPath specifies the mount path in the container namespace.
	ContainerPath string
	// HostPath specifies the mount path in the host namespace.
	HostPath string
	// If set, the mount is read-only.
	Readonly bool
}

// VMVolumeDevice denotes a raw block device mapping within a VM which
// is used for block PVs.
type VMVolumeDevice struct {
	// DevicePath specifies the path to the device inside the VM.
	DevicePath string
	// HostPath specifies the mount path in the host namespace.
	HostPath string
}

// VMConfig contains the information needed to start create a VM
// TODO: use this struct to store VM metadata.
type VMConfig struct {
	// Id of the containing pod sandbox.
	PodSandboxID string
	// Name of the containing pod sandbox.
	PodName string
	// Namespace of the containing pod sandbox.
	PodNamespace string
	// Name of the container (VM).
	Name string
	// Image to use for the VM.
	Image string
	// Attempt is the number of container creation attempts before this one.
	Attempt uint32
	// Memory limit in bytes. Default: 0 (not specified).
	MemoryLimitInBytes int64
	// CPU shares (relative weight vs. other containers). Default: 0 (not specified).
	CPUShares int64
	// CPU CFS (Completely Fair Scheduler) period. Default: 0 (not specified).
	CPUPeriod int64
	// CPU CFS (Completely Fair Scheduler) quota. Default: 0 (not specified).
	CPUQuota int64
	// Annotations for the containing pod.
	PodAnnotations map[string]string
	// Annotations for the container.
	ContainerAnnotations map[string]string
	// Labels for the container.
	ContainerLabels map[string]string
	// Parsed representation of pod annotations. Populated by LoadAnnotations() call.
	ParsedAnnotations *VirtletAnnotations
	// Domain UUID (set by the CreateContainer).
	// TODO: this field should be moved to VMStatus
	DomainUUID string
	// Environment variables to set in the VM.
	Environment []VMKeyValue
	// Host directories corresponding to the volumes which are to.
	// be mounted inside the VM
	Mounts []VMMount
	// Host block devices that should be made available inside the VM.
	// This is used for block PVs.
	VolumeDevices []VMVolumeDevice
	// ContainerSideNetwork stores info about container side network configuration.
	ContainerSideNetwork *network.ContainerSideNetwork
	// Path to the directory on the host in which container log files are
	// stored.
	LogDirectory string
	// Path relative to LogDirectory for container to store the
	// log (STDOUT and STDERR) on the host.
	LogPath string
}

// LoadAnnotations parses pod annotations in the VM config an
// populates the ParsedAnnotations field.
func (c *VMConfig) LoadAnnotations() error {
	ann, err := loadAnnotations(c.PodNamespace, c.PodAnnotations)
	if err != nil {
		return err
	}
	c.ParsedAnnotations = ann
	return nil
}

// ContainerFilter is used to filter containers.
// All those fields are combined with 'AND'
type ContainerFilter struct {
	// ID of the container.
	Id string
	// State of the container.
	State *ContainerState
	// ID of the PodSandbox.
	PodSandboxID string
	// LabelSelector to select matches.
	// Only api.MatchLabels is supported for now and the requirements
	// are ANDed. MatchExpressions is not supported yet.
	LabelSelector map[string]string
}
