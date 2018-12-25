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
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	cgroupfs = "/sys/fs/cgroup"
)

// GetProcessControllers returns the mapping between controller types and
// their paths inside cgroup fs for the specified PID
func GetProcessControllers(pid interface{}) (map[string]string, error) {
	sPid := utils.Stringify(pid)

	file, err := os.Open(filepath.Join("/proc", sPid, "cgroup"))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	ctrls := make(map[string]string)

	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// strip eol
		line = strings.Trim(line, "\n")

		// split entries like:
		// "6:memory:/user.slice/user-xxx.slice/session-xx.scope"
		parts := strings.SplitN(line, ":", 3)

		// use second part as controller name and third as its path
		ctrls[parts[1]] = parts[2]
	}

	return ctrls, nil
}

// Controller represents a named controller for a process
type Controller struct {
	name string
	path string
}

// GetProcessController returns a named resource Controller for the specified PID
func GetProcessController(pid interface{}, controllerName string) (*Controller, error) {
	controllers, err := GetProcessControllers(pid)
	if err != nil {
		return nil, err
	}

	controllerPath, ok := controllers[controllerName]
	if !ok {
		return nil, fmt.Errorf("controller %q for process %v not found", controllerName, pid)
	}

	return &Controller{
		name: controllerName,
		path: controllerPath,
	}, nil
}

// Set sets the value of a controller setting
func (c *Controller) Set(name string, value interface{}) error {
	sValue := utils.Stringify(value)
	return ioutil.WriteFile(filepath.Join(cgroupfs, c.name, c.path, c.name+"."+name), []byte(sValue), 0644)
}

// MoveCGroup move the process into controller path's cgroup.procs
func MoveCGroup(pid interface{}, controller, path string) error {
	return ioutil.WriteFile(filepath.Join(cgroupfs, controller, path, "cgroup.procs"), []byte(utils.Stringify(pid)), 0644)
}
