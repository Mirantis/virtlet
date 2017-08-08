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
	"os"
	"path/filepath"
	"strings"
)

func (v *VirtualizationTool) GarbageCollect() error {
	ids, err := v.retrieveListOfContainerIDs()
	if err != nil {
		return err
	}

	if err := v.removeOrphanDomains(ids); err != nil {
		return err
	}

	if err := v.removeOrphanRootVolumes(ids); err != nil {
		return err
	}

	if err := v.removeOrphanQcow2Volumes(ids); err != nil {
		return err
	}

	if err := v.removeOrphanNoCloudImages(ids); err != nil {
		return err
	}

	return nil
}

func (v *VirtualizationTool) retrieveListOfContainerIDs() ([]string, error) {
	var containerIDs []string

	sandboxes, err := v.metadataStore.ListPodSandboxes(nil)
	if err != nil {
		return nil, err
	}

	for _, sandbox := range sandboxes {
		containers, err := v.metadataStore.ListPodContainers(sandbox.GetID())
		if err != nil {
			return nil, err
		}
		for _, container := range containers {
			containerIDs = append(containerIDs, container.GetID())
		}
	}

	return containerIDs, nil
}

func inList(list []string, filter func(string) bool) bool {
	for _, element := range list {
		if filter(element) {
			return true
		}
	}
	return false
}

func (v *VirtualizationTool) removeOrphanDomains(ids []string) error {
	domains, err := v.domainConn.ListDomains()
	if err != nil {
		return err
	}

	for _, domain := range domains {
		name, err := domain.Name()
		if err != nil {
			return err
		}

		filter := func(id string) bool {
			return strings.HasPrefix("virtlet-"+id, name)
		}

		if !inList(ids, filter) {
			d, err := v.DomainConnection().LookupDomainByName(name)
			if err != nil {
				return err
			}

			// ignore errors from stopping domain
			d.Destroy()
			if err := d.Undefine(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (v *VirtualizationTool) removeOrphanRootVolumes(ids []string) error {
	volumes, err := v.volumePool.ListAllVolumes()
	if err != nil {
		return err
	}

	for _, volume := range volumes {
		path, err := volume.Path()
		if err != nil {
			return err
		}

		filename := filepath.Base(path)
		filter := func(id string) bool {
			return "virtlet_root_"+id == filename
		}

		if !inList(ids, filter) {
			if err := volume.Remove(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (v *VirtualizationTool) removeOrphanQcow2Volumes(ids []string) error {
	volumes, err := v.volumePool.ListAllVolumes()
	if err != nil {
		return err
	}

	for _, volume := range volumes {
		path, err := volume.Path()
		if err != nil {
			return err
		}

		filename := filepath.Base(path)
		filter := func(id string) bool {
			return strings.HasPrefix("virtlet-"+id, filename)
		}

		if !inList(ids, filter) {
			if err := volume.Remove(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (v *VirtualizationTool) removeOrphanNoCloudImages(ids []string) error {
	files, err := filepath.Glob(filepath.Join(nocloudIsoDir, "nocloud-*.iso"))
	if err != nil {
		return err
	}

	for _, path := range files {
		filename := filepath.Base(path)

		filter := func(id string) bool {
			return filename == "nocloud-"+id+".iso"
		}

		if !inList(ids, filter) {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}
