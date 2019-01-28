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

package cgroups

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	cgroupfs = "/sys/fs/cgroup"
)

// Controller represents a named controller for a process
type Controller struct {
	fm   utils.FilesManipulator
	name string
	path string
}

// CgroupsManager provides an interface to operate on linux cgroups
type CgroupsManager interface {
	// GetProcessControllers returns the mapping between controller types and
	// their paths inside cgroup fs for the specified PID.
	GetProcessControllers() (map[string]string, error)
	// GetProcessController returns a named resource Controller for the specified PID.
	GetProcessController(controllerName string) (*Controller, error)
	// MoveProcess move the process to the path under a cgroup controller
	MoveProcess(controller, path string) error
}

// RealCgroupsManager provides an implementation of CgroupsManager which is
// using default linux system paths to access info about cgroups for processes.
type RealCgroupsManager struct {
	fm  utils.FilesManipulator
	pid string
}

var _ CgroupsManager = &RealCgroupsManager{}

// NewRealCgroupsManager returns instance of RealCgroupsManager
func NewCgroupsManager(pid interface{}, fm utils.FilesManipulator) CgroupsManager {
	if fm == nil {
		fm = utils.DefaultFilesManipulator
	}
	return &RealCgroupsManager{fm: fm, pid: utils.Stringify(pid)}
}

// GetProcessControllers is an implementation of GetProcessControllers method
// of CgroupsManager interface.
func (c *RealCgroupsManager) GetProcessControllers() (map[string]string, error) {
	fr, err := c.fm.FileReader(filepath.Join("/proc", c.pid, "cgroup"))
	if err != nil {
		return nil, err
	}
	defer fr.Close()

	ctrls := make(map[string]string)

	for {
		line, err := fr.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
		}

		// strip eol
		line = strings.Trim(line, "\n")
		if line == "" {
			break
		}

		// split entries like:
		// "6:memory:/user.slice/user-xxx.slice/session-xx.scope"
		parts := strings.SplitN(line, ":", 3)

		// use second part as controller name and third as its path
		ctrls[parts[1]] = parts[2]

		if err == io.EOF {
			break
		}
	}

	return ctrls, nil
}

// GetProcessController is an implementation of GetProcessController method
// of CgroupsManager interface.
func (c *RealCgroupsManager) GetProcessController(controllerName string) (*Controller, error) {
	controllers, err := c.GetProcessControllers()
	if err != nil {
		return nil, err
	}

	controllerPath, ok := controllers[controllerName]
	if !ok {
		return nil, fmt.Errorf("controller %q for process %v not found", controllerName, c.pid)
	}

	return &Controller{
		fm:   c.fm,
		name: controllerName,
		path: controllerPath,
	}, nil
}

// MoveProcess implements MoveProcess method of CgroupsManager
func (c *RealCgroupsManager) MoveProcess(controller, path string) error {
	return c.fm.WriteFile(
		filepath.Join(cgroupfs, controller, path, "cgroup.procs"),
		[]byte(utils.Stringify(c.pid)),
		0644,
	)
}

// Set sets the value of a controller setting
func (c *Controller) Set(name string, value interface{}) error {
	return c.fm.WriteFile(
		filepath.Join(cgroupfs, c.name, c.path, c.name+"."+name),
		[]byte(utils.Stringify(value)),
		0644,
	)
}
