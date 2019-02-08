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

package libvirttools

import (
	"fmt"
	"io"
	"os"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	vconfig "github.com/Mirantis/virtlet/pkg/config"
	"github.com/Mirantis/virtlet/pkg/utils/cgroups"
)

const (
	procfsLocation      = "/proc"
	emulatorProcessName = "qemu-system-x86_64"
)

// UpdateCpusetsInContainerDefinition updates libvirt domain definition for the VM
// setting the environment variable which is used by vmwrapper to pin to the specified cpuset
func (v *VirtualizationTool) UpdateCpusetsInContainerDefinition(containerID, cpusets string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return err
	}

	domainxml, err := domain.XML()
	if err != nil {
		return err
	}

	found := false
	envvars := domainxml.QEMUCommandline.Envs
	for _, envvar := range envvars {
		if envvar.Name == vconfig.CpusetsEnvVarName {
			envvar.Value = cpusets
			found = true
		}
	}
	if !found && cpusets != "" {
		domainxml.QEMUCommandline.Envs = append(envvars, libvirtxml.DomainQEMUCommandlineEnv{
			Name:  vconfig.CpusetsEnvVarName,
			Value: cpusets,
		})
	}

	if err := domain.Undefine(); err != nil {
		return err
	}

	_, err = v.domainConn.DefineDomain(domainxml)
	return err
}

// UpdateCpusetsForEmulatorProcess looks through /proc for emulator process
// to find its cgroup manager for cpusets then uses it to adjust the setting
func (v *VirtualizationTool) UpdateCpusetsForEmulatorProcess(containerID, cpusets string) (bool, error) {
	pidFilePath, err := v.getEmulatorPidFileLocation(containerID)
	if err != nil {
		return false, err
	}

	f, err := v.fsys.GetDelimitedReader(pidFilePath)
	if err != nil {
		// File not found - so there is no emulator yet
		if _, ok := err.(*os.PathError); ok {
			return false, nil
		}
		return false, err
	}
	defer f.Close()

	// there should be only a single line without eol, but use eol as
	// a marker to read data to EOF.
	pid, err := f.ReadString('\n')
	if err != nil {
		if err != io.EOF {
			return false, err
		}
	}

	cm := cgroups.NewManager(pid, v.fsys)
	controller, err := cm.GetProcessController("cpuset")
	if err != nil {
		return false, err
	}

	if err := controller.Set("cpus", cpusets); err != nil {
		return false, err
	}
	return true, nil
}

func (v *VirtualizationTool) getEmulatorPidFileLocation(containerID string) (string, error) {
	container, err := v.metadataStore.Container(containerID).Retrieve()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"/run/libvirt/qemu/virtlet-%s-%s.pid",
		containerID[:13], container.Config.PodName,
	), nil
}
