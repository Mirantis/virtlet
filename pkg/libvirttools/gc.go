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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mirantis/virtlet/pkg/blockdev"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

const (
	configFilenameTemplate = "config-*.iso"
)

// GarbageCollect retrieves from metadata store list of container ids,
// passes it to all GC submodules, collecting from them list of
// possible errors, which is returned to outer scope
func (v *VirtualizationTool) GarbageCollect() (allErrors []error) {
	ids, fatal, errors := v.retrieveListOfContainerIDs()
	if errors != nil {
		allErrors = append(allErrors, errors...)
	}
	if fatal {
		return
	}

	allErrors = append(allErrors, v.removeOrphanDomains(ids)...)
	allErrors = append(allErrors, v.removeOrphanRootVolumes(ids)...)
	allErrors = append(allErrors, v.removeOrphanQcow2Volumes(ids)...)
	allErrors = append(allErrors, v.removeOrphanConfigImages(ids, configIsoDir)...)
	allErrors = append(allErrors, v.removeOrphanVirtualBlockDevices(ids, "", "")...)

	return
}

func (v *VirtualizationTool) retrieveListOfContainerIDs() ([]string, bool, []error) {
	var containerIDs []string

	sandboxes, err := v.metadataStore.ListPodSandboxes(nil)
	if err != nil {
		return nil, true, []error{
			fmt.Errorf("cannot list pod sandboxes: %v", err),
		}
	}

	var allErrors []error
	for _, sandbox := range sandboxes {
		if err := v.checkSandboxNetNs(sandbox); err != nil {
			allErrors = append(allErrors, err)
			continue
		}

		containers, err := v.metadataStore.ListPodContainers(sandbox.GetID())
		if err != nil {
			allErrors = append(
				allErrors,
				fmt.Errorf(
					"cannot list containers for pod %s: %v",
					sandbox.GetID(),
					err,
				),
			)
			continue
		}
		for _, container := range containers {
			containerIDs = append(containerIDs, container.GetID())
		}
	}

	return containerIDs, false, allErrors
}

func (v *VirtualizationTool) checkSandboxNetNs(sandbox metadata.PodSandboxMetadata) error {
	sinfo, err := sandbox.Retrieve()
	if err != nil {
		return err
	}

	if !v.mountPointChecker.IsPathAnNs(sinfo.ContainerSideNetwork.NsPath) {
		// NS didn't found, need RunSandbox again
		if err := sandbox.Save(func(s *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
			if s != nil {
				s.State = types.PodSandboxState_SANDBOX_NOTREADY
			}
			return s, nil
		}); err != nil {
			return err
		}
	}

	return nil
}

func inList(list []string, filter func(string) bool) bool {
	for _, element := range list {
		if filter(element) {
			return true
		}
	}
	return false
}

func (v *VirtualizationTool) removeOrphanDomains(ids []string) []error {
	domains, err := v.domainConn.ListDomains()
	if err != nil {
		return []error{fmt.Errorf("cannot list domains: %v", err)}
	}

	var allErrors []error
	for _, domain := range domains {
		name, err := domain.Name()
		if err != nil {
			allErrors = append(
				allErrors,
				fmt.Errorf("cannot retrieve domain name: %v", err),
			)
		}

		filter := func(id string) bool {
			return strings.HasPrefix(name, "virtlet-"+id[:13])
		}

		if !inList(ids, filter) {
			d, err := v.DomainConnection().LookupDomainByName(name)
			if err != nil {
				allErrors = append(
					allErrors,
					fmt.Errorf(
						"cannot lookup domain '%s' by name: %v",
						name,
						err,
					),
				)
				continue
			}

			// ignore errors from stopping domain - it can be (and probably is) already stopped
			d.Destroy()
			if err := d.Undefine(); err != nil {
				allErrors = append(
					allErrors,
					fmt.Errorf(
						"cannot undefine domain '%s': %v",
						name,
						err,
					),
				)
			}
		}
	}

	return allErrors
}

func (v *VirtualizationTool) removeOrphanRootVolumes(ids []string) []error {
	volumePool, err := v.StoragePool()
	if err != nil {
		return []error{fmt.Errorf("cannot get the storage pool: %v", err)}
	}
	volumes, err := volumePool.ListVolumes()
	if err != nil {
		return []error{fmt.Errorf("cannot list libvirt volumes: %v", err)}
	}

	var allErrors []error
	for _, volume := range volumes {
		path, err := volume.Path()
		if err != nil {
			allErrors = append(
				allErrors,
				fmt.Errorf("cannot retrieve volume path: %v", err),
			)
			continue
		}

		filename := filepath.Base(path)
		filter := func(id string) bool {
			return "virtlet_root_"+id == filename
		}

		if strings.HasPrefix(filename, "virtlet_root_") && !inList(ids, filter) {
			if err := volume.Remove(); err != nil {
				allErrors = append(
					allErrors,
					fmt.Errorf(
						"cannot remove volume with path '%s': %v",
						path,
						err,
					),
				)
			}
		}
	}

	return allErrors
}

func (v *VirtualizationTool) removeOrphanQcow2Volumes(ids []string) []error {
	volumePool, err := v.StoragePool()
	if err != nil {
		return []error{fmt.Errorf("cannot get the storage pool: %v", err)}
	}
	volumes, err := volumePool.ListVolumes()
	if err != nil {
		return []error{fmt.Errorf("cannot list domains: %v", err)}
	}

	var allErrors []error
	for _, volume := range volumes {
		path, err := volume.Path()
		if err != nil {
			allErrors = append(
				allErrors,
				fmt.Errorf("cannot retrieve volume path: %v", err),
			)
			continue
		}

		filename := filepath.Base(path)
		filter := func(id string) bool {
			return strings.HasPrefix(filename, "virtlet-"+id)
		}

		if strings.HasPrefix(filename, "virtlet-") && !inList(ids, filter) {
			if err := volume.Remove(); err != nil {
				allErrors = append(
					allErrors,
					fmt.Errorf(
						"cannot remove volume with path '%s': %v",
						path,
						err,
					),
				)
			}
		}
	}

	return allErrors
}

func (v *VirtualizationTool) removeOrphanConfigImages(ids []string, directory string) []error {
	files, err := filepath.Glob(filepath.Join(directory, configFilenameTemplate))
	if err != nil {
		return []error{
			fmt.Errorf(
				"error while globbing '%s' files in '%s' directory: %v",
				configFilenameTemplate,
				configIsoDir,
				err,
			),
		}
	}

	var allErrors []error
	for _, path := range files {
		filename := filepath.Base(path)

		filter := func(id string) bool {
			return filename == "config-"+id+".iso"
		}

		if strings.HasPrefix(filename, "config-") && strings.HasSuffix(filename, ".iso") && !inList(ids, filter) {
			if err := os.Remove(path); err != nil {
				allErrors = append(
					allErrors,
					fmt.Errorf(
						"cannot remove volume with path '%s': %v",
						path,
						err,
					),
				)
			}
		}
	}

	return allErrors
}

func (v *VirtualizationTool) removeOrphanVirtualBlockDevices(ids []string, devPath, sysfsPath string) []error {
	idsInUse := make(map[string]bool)
	for _, id := range ids {
		idsInUse[id] = true
	}
	ldh := blockdev.NewLogicalDeviceHandler(v.Commander(), devPath, sysfsPath)
	dmNames, err := ldh.ListVirtletLogicalDevices()
	if err != nil {
		return []error{err}
	}

	var allErrors []error
	for _, dmName := range dmNames {
		if !strings.HasPrefix(dmName, blockdev.VirtletLogicalDevicePrefix) {
			panic("bad dmname " + dmName)
		}
		id := dmName[len(blockdev.VirtletLogicalDevicePrefix):]
		if idsInUse[id] {
			continue
		}
		if err := ldh.Unmap(dmName); err != nil {
			allErrors = append(
				allErrors,
				fmt.Errorf("error unmapping %q: %v", dmName, err))
		}
	}

	return allErrors
}
