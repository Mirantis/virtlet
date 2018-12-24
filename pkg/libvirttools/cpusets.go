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
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	// TODO: replace iterating over the procfs with reading pid from
	// /run/libvirt/qemu/virtlet-CONTAINER_ID[:12]-DOMAIN_NAME.pid
	d, err := os.Open(procfsLocation)
	if err != nil {
		return false, err
	}
	defer d.Close()

	entries, err := d.Readdirnames(-1)
	if err != nil {
		return false, err
	}

	for _, name := range entries {
		_, err := strconv.ParseInt(name, 10, 32)
		if err != nil {
			// skip non numeric names
			continue
		}

		isContainerPid, err := isEmulatorPid(name, containerID)
		if err != nil {
			return false, err
		}

		if isContainerPid {
			controller, err := cgroups.GetProcessController(name, "cpuset")
			if err != nil {
				return false, err
			}

			if err := controller.Set("cpus", cpusets); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	return false, nil
}

func isEmulatorPid(pid, containerID string) (bool, error) {
	data, err := ioutil.ReadFile(filepath.Join(procfsLocation, pid, "cmdline"))
	if err != nil {
		return false, err
	}

	cmdline := bytes.Split(data, []byte{0})

	if string(cmdline[0]) != emulatorProcessName {
		return false, nil
	}

	searchTerm := "virtlet-" + containerID[:12]
	for _, param := range cmdline {
		if strings.Contains(string(param), searchTerm) {
			return true, nil
		}
	}

	return false, nil
}
