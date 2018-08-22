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

package flexvolume

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	uuidOptionsKey     = "uuid"
	partOptionsKey     = "part"
	flexvolumeDataFile = "virtlet-flexvolume.json"
)

// flexVolumeDebug indicates whether flexvolume debugging should be enabled
var flexVolumeDebug = false

func init() {
	// XXX: invent a better way to decide whether debugging should
	// be used for flexvolume driver. For now we only enable it if
	// Docker-in-Docker env is used
	if fi, err := os.Stat("/dind/flexvolume_driver"); err == nil && !fi.IsDir() {
		flexVolumeDebug = true
	}
}

type nullMounter struct{}

func (m *nullMounter) Mount(source string, target string, fstype string) error {
	return nil
}

func (m *nullMounter) Unmount(target string) error {
	return nil
}

var NullMounter = &nullMounter{}

type UuidGen func() string

type FlexVolumeDriver struct {
	uuidGen UuidGen
	mounter utils.Mounter
}

func NewFlexVolumeDriver(uuidGen UuidGen, mounter utils.Mounter) *FlexVolumeDriver {
	return &FlexVolumeDriver{uuidGen: uuidGen, mounter: mounter}
}

func (d *FlexVolumeDriver) populateVolumeDir(targetDir string, opts map[string]interface{}) error {
	return utils.WriteJSON(filepath.Join(targetDir, flexvolumeDataFile), opts, 0700)
}

// The following functions are not currently needed, but still
// keeping them to make it easier to actually implement them

// Invocation: <driver executable> init
func (d *FlexVolumeDriver) init() (map[string]interface{}, error) {
	return nil, nil
}

// Invocation: <driver executable> attach <json options> <node name>
func (d *FlexVolumeDriver) attach(jsonOptions, nodeName string) (map[string]interface{}, error) {
	return nil, nil
}

// Invocation: <driver executable> detach <mount device> <node name>
func (d *FlexVolumeDriver) detach(mountDev, nodeName string) (map[string]interface{}, error) {
	return nil, nil
}

// Invocation: <driver executable> waitforattach <mount device> <json options>
func (d *FlexVolumeDriver) waitForAttach(mountDev, jsonOptions string) (map[string]interface{}, error) {
	return map[string]interface{}{"device": mountDev}, nil
}

// Invocation: <driver executable> isattached <json options> <node name>
func (d *FlexVolumeDriver) isAttached(jsonOptions, nodeName string) (map[string]interface{}, error) {
	return map[string]interface{}{"attached": true}, nil
}

//Invocation: <driver executable> mount <target mount dir> <json options>
func (d *FlexVolumeDriver) mount(targetMountDir, jsonOptions string) (map[string]interface{}, error) {
	var opts map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOptions), &opts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json options: %v", err)
	}
	opts[uuidOptionsKey] = d.uuidGen()
	if err := os.MkdirAll(targetMountDir, 0700); err != nil {
		return nil, fmt.Errorf("os.MkDirAll(): %v", err)
	}

	// Here we populate the volume directory twice.
	// That's because we need to do tmpfs mount to make kubelet happy -
	// it will not try to unmount the volume if it doesn't detect mount
	// point on the flexvolume dir, and using 'mount --bind' is not enough
	// because kubelet's mount point detection doesn't see bind mounts.
	// The problem is that hostPaths are mounted as private (no mount
	// propagation) and so tmpfs content is not visible inside Virtlet
	// pod. So, in order to avoid unneeded confusion down the road,
	// we place flexvolume contents to the volume dir twice,
	// both directly and onto the freshly mounted tmpfs.

	if err := d.populateVolumeDir(targetMountDir, opts); err != nil {
		return nil, err
	}

	if err := d.mounter.Mount("tmpfs", targetMountDir, "tmpfs"); err != nil {
		return nil, fmt.Errorf("error mounting tmpfs at %q: %v", targetMountDir, err)
	}

	done := false
	defer func() {
		// try to unmount upon error or panic
		if !done {
			d.mounter.Unmount(targetMountDir)
		}
	}()

	if err := d.populateVolumeDir(targetMountDir, opts); err != nil {
		return nil, err
	}

	done = true
	return nil, nil
}

// Invocation: <driver executable> unmount <mount dir>
func (d *FlexVolumeDriver) unmount(targetMountDir string) (map[string]interface{}, error) {
	if err := d.mounter.Unmount(targetMountDir); err != nil {
		return nil, fmt.Errorf("unmount %q: %v", targetMountDir, err.Error())
	}

	if err := os.RemoveAll(targetMountDir); err != nil {
		return nil, fmt.Errorf("os.RemoveAll(): %v", err.Error())
	}

	return nil, nil
}

type driverOp func(*FlexVolumeDriver, []string) (map[string]interface{}, error)

type cmdInfo struct {
	numArgs int
	run     driverOp
}

var commands = map[string]cmdInfo{
	"init": {
		0, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.init()
		},
	},
	"attach": {
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.attach(args[0], args[1])
		},
	},
	"detach": {
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.detach(args[0], args[1])
		},
	},
	"waitforattach": {
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.waitForAttach(args[0], args[1])
		},
	},
	"isattached": {
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.isAttached(args[0], args[1])
		},
	},
	"mount": {
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.mount(args[0], args[1])
		},
	},
	"unmount": {
		1, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.unmount(args[0])
		},
	},
}

func (d *FlexVolumeDriver) doRun(args []string) (map[string]interface{}, error) {
	if len(args) == 0 {
		return nil, errors.New("no arguments passed to flexvolume driver")
	}
	nArgs := len(args) - 1
	op := args[0]
	if cmdInfo, found := commands[op]; found {
		if cmdInfo.numArgs == nArgs {
			return cmdInfo.run(d, args[1:])
		} else {
			return nil, fmt.Errorf("unexpected number of args %d (expected %d) for operation %q", nArgs, cmdInfo.numArgs, op)
		}
	} else {
		return map[string]interface{}{
			"status": "Not supported",
		}, nil
	}
}

func (d *FlexVolumeDriver) Run(args []string) string {
	r := formatResult(d.doRun(args))

	if flexVolumeDebug {
		// This is for debugging purposes only.
		// The problem is that kubelet grabs CombinedOutput() from the process
		// and tries to parse it as JSON (need to recheck this,
		// maybe submit a PS to fix it)
		f, err := os.OpenFile("/tmp/flexvolume.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0666)
		if err == nil {
			defer f.Close()
			fmt.Fprintf(f, "flexvolume %s -> %s\n", strings.Join(args, " "), r)
		}
	}

	return r
}

func formatResult(fields map[string]interface{}, err error) string {
	var data map[string]interface{}
	if err != nil {
		data = map[string]interface{}{
			"status":  "Failure",
			"message": err.Error(),
		}
	} else {
		data = map[string]interface{}{
			"status": "Success",
		}
		for k, v := range fields {
			data[k] = v
		}
	}
	s, err := json.Marshal(data)
	if err != nil {
		panic("error marshalling the data")
	}
	return string(s) + "\n"
}

// GetFlexvolumeInfo tries to extract flexvolume uuid and partition
// number from the specified directory. Negative partition number
// means that no partition number was specified.
func GetFlexvolumeInfo(dir string) (string, int, error) {
	dataFile := filepath.Join(dir, flexvolumeDataFile)
	var opts map[string]interface{}
	if err := utils.ReadJSON(dataFile, &opts); err != nil {
		return "", 0, fmt.Errorf("can't read flexvolume data file %q: %v", dataFile, err)
	}
	uuidRaw, found := opts[uuidOptionsKey]
	if !found {
		return "", 0, fmt.Errorf("%q: flexvolume doesn't have an uuid", dataFile)
	}
	uuid, ok := uuidRaw.(string)
	if !ok {
		return "", 0, fmt.Errorf("%q: flexvolume uuid is not a string", dataFile)
	}
	part := -1
	partRaw, found := opts[partOptionsKey]
	if found {
		partStr, ok := partRaw.(string)
		if !ok {
			return "", 0, fmt.Errorf("%q: 'part' value is not a string", dataFile)
		}
		var err error
		part, err = strconv.Atoi(partStr)
		if err != nil {
			return "", 0, fmt.Errorf("%q: malformed 'part' value (partition number): %q: %v", dataFile, partRaw, err)
		}
	}
	return uuid, part, nil
}
