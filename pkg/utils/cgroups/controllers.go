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

// GetProcessControllers returns mapping between controller types and theirs
// paths inside cgroups fs mount for particular process identificator
func GetProcessControllers(pid interface{}) (map[string]string, error) {
	sPid, err := utils.Stringify(pid)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(filepath.Join("/proc", sPid, "cgroups"))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	var ctrls map[string]string

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

// Controller represents named controller for particular process
type Controller struct {
	name string
	path string
}

// GetProcessController returns named resource Controller for given process identificator
func GetProcessController(pid interface{}, controllerName string) (*Controller, error) {
	sPid, err := utils.Stringify(pid)
	if err != nil {
		return nil, err
	}

	controllers, err := GetProcessControllers(pid)
	if err != nil {
		return nil, err
	}

	controllerPath, ok := controllers[controllerName]
	if !ok {
		return nil, fmt.Errorf("controller %q for process %q not found", controllerName, sPid)
	}

	return &Controller{
		name: controllerName,
		path: controllerPath,
	}, nil
}

// Set sets value for particular setting using this controller
func (c *Controller) Set(name string, value interface{}) error {
	sValue, err := utils.Stringify(value)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filepath.Join(cgroupfs, c.name, c.path, c.name+"."+name), []byte(sValue), 0644)
}
