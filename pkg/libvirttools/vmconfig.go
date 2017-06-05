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

// VMConfig contains the information needed to start create a VM
// TODO: use this struct to store VM metadata
type VMConfig struct {
	// Id of the containing pod sandbox
	PodSandboxId string
	// Name of the containing pod sandbox
	PodName string
	// Namespace of the containing pod sandbox
	PodNamespace string
	// Name of the container (VM)
	Name string
	// Image to use for the VM
	Image string
	// Attempt is the number of container creation attempts before this one
	Attempt uint32
	// Memory limit in bytes. Default: 0 (not specified)
	MemoryLimitInBytes int64
	// CPU shares (relative weight vs. other containers). Default: 0 (not specified)
	CpuShares int64
	// CPU CFS (Completely Fair Scheduler) period. Default: 0 (not specified)
	CpuPeriod int64
	// CPU CFS (Completely Fair Scheduler) quota. Default: 0 (not specified)
	CpuQuota int64
	// Annotations for the containing pod
	PodAnnotations map[string]string
	// Annotations for the container
	ContainerAnnotations map[string]string
	// Labels for the container
	ContainerLabels map[string]string
	// Parsed representation of pod annotations. Populated by LoadAnnotations() call
	ParsedAnnotations *VirtletAnnotations
	// Domain UUID (set by the CreateContainer)
	// TODO: this field should be moved to VMStatus
	DomainUUID string
	// A temporary file or directory associated with this domain.
	// Currently used by nocloudVolume
	// TODO: this field should be moved to VMStatus.
	// Better, there should be a directory associated with each domain
	// storing its metadata as json file and also containing nocloud
	// iso image. This directory should be removed together with the
	// domain
	TempFile string
}

// LoadAnnotations parses pod annotations in the VM config an
// populates the ParsedAnnotations field.
func (c *VMConfig) LoadAnnotations() error {
	ann, err := LoadAnnotations(c.PodAnnotations)
	if err != nil {
		return err
	}
	c.ParsedAnnotations = ann
	return nil
}
