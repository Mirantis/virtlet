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
	"io/ioutil"
	"os"
	"strings"

	uuid "github.com/nu7hatch/gouuid"
)

type volumeOpts struct {
	Monitor  string `json:"monitor"`
	Pool     string `json:"pool"`
	Volume   string `json:"volume"`
	Secret   string `json:"secret"`
	User     string `json:user`
	Protocol string `json:protocol`
}

func newUuid() string {
	u, err := uuid.NewV4()
	if err != nil {
		panic("can't generate UUID")
	}
	return u.String()
}

type UuidGen func() string

type FlexVolumeDriver struct {
	uuidGen UuidGen
}

func NewFlexVolumeDriver(uuidGen UuidGen) *FlexVolumeDriver {
	if uuidGen == nil {
		uuidGen = newUuid
	}
	return &FlexVolumeDriver{uuidGen: uuidGen}
}

func (d *FlexVolumeDriver) addVolumeDefinitions(target string, opts volumeOpts) error {
	uuid := d.uuidGen()

	secretXML := `
<secret ephemeral='no' private='no'>
  <uuid>%s</uuid>
  <usage type='ceph'>
    <name>%s</name>
  </usage>
</secret>
`
	if err := ioutil.WriteFile(target+"/secret.xml", []byte(fmt.Sprintf(secretXML, uuid, opts.User)), 0644); err != nil {
		return err
	}

	// Will be removed right after creating appropriate secret in libvirt
	if err := ioutil.WriteFile(target+"/key", []byte(opts.Secret), 0644); err != nil {
		return err
	}

	diskXML := `
<disk type="network" device="disk">
  <driver name="qemu" type="raw"/>
  <auth username="%s">
    <secret type="ceph" uuid="%s"/>
  </auth>
  <source protocol="rbd" name="%s/%s">
    <host name="%s" port="%s"/>
  </source>
  <target dev="%s" bus="virtio"/>
</disk>
`
	pairIPPort := strings.Split(opts.Monitor, ":")
	if len(pairIPPort) != 2 {
		return fmt.Errorf("invalid format of ceph monitor setting: %s. Expected in form ip:port", opts.Monitor)
	}
	// Note: target dev name wiil be specified later by virtlet diring combining domain xml definition
	if err := ioutil.WriteFile(target+"/disk.xml", []byte(fmt.Sprintf(diskXML, opts.User, uuid, opts.Pool, opts.Volume, pairIPPort[0], pairIPPort[1], "%s")), 0644); err != nil {
		return err
	}
	return nil
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

//Invocation: <driver executable> mount <target mount dir> <mount device> <json options>
func (d *FlexVolumeDriver) mount(targetMountDir, jsonOptions string) (map[string]interface{}, error) {
	var opts volumeOpts
	if err := json.Unmarshal([]byte(jsonOptions), &opts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json options: %v", err)
	}
	if err := os.MkdirAll(targetMountDir, 0700); err != nil {
		return nil, fmt.Errorf("os.MkDirAll(): %v", err)
	}
	if err := d.addVolumeDefinitions(targetMountDir, opts); err != nil {
		return nil, err
	}
	return nil, nil
}

// Invocation: <driver executable> unmount <mount dir>
func (d *FlexVolumeDriver) unmount(targetMountDir string) (map[string]interface{}, error) {
	if err := os.RemoveAll(targetMountDir); err != nil {
		return nil, fmt.Errorf("os.RemoveAll(): %v", err.Error())
	}

	return nil, nil
}

// Invocation: <driver executable> getvolumename <json options>
func (d *FlexVolumeDriver) getVolumeName(jsonOptions string) (map[string]interface{}, error) {
	var opts volumeOpts
	if err := json.Unmarshal([]byte(jsonOptions), &opts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json options: %v", err)
	}
	r := []string{}
	monStr := strings.Replace(opts.Monitor, ":", "/", -1)
	for _, s := range []string{monStr, opts.Pool, opts.Volume, opts.User, opts.Protocol} {
		if s != "" {
			r = append(r, s)
		}
	}
	if len(r) == 0 {
		return nil, fmt.Errorf("invalid flexvolume definition")
	}
	return map[string]interface{}{"volumeName": strings.Join(r, "/")}, nil
}

type driverOp func(*FlexVolumeDriver, []string) (map[string]interface{}, error)

type cmdInfo struct {
	numArgs int
	run     driverOp
}

var commands = map[string]cmdInfo{
	"init": cmdInfo{
		0, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.init()
		},
	},
	"attach": cmdInfo{
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.attach(args[0], args[1])
		},
	},
	"detach": cmdInfo{
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.detach(args[0], args[1])
		},
	},
	"waitforattach": cmdInfo{
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.waitForAttach(args[0], args[1])
		},
	},
	"isattached": cmdInfo{
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.isAttached(args[0], args[1])
		},
	},
	"getvolumename": cmdInfo{
		1, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.getVolumeName(args[0])
		},
	},
	"mount": cmdInfo{
		2, func(d *FlexVolumeDriver, args []string) (map[string]interface{}, error) {
			return d.mount(args[0], args[1])
		},
	},
	"unmount": cmdInfo{
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

	// Uncomment the following for debugging.
	// TODO: make this configurable somehow.
	// The problem is that kubelet grabs CombinedOutput() from the process
	// and tries to parse it as JSON (need to recheck this,
	// maybe submit a PS to fix it)

	// f, err := os.OpenFile("/tmp/flexvolume.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0666)
	// if err != nil {
	// 	panic(err)
	// }

	// defer f.Close()

	// fmt.Fprintf(f, "flexvolume %s -> %s\n", strings.Join(args, " "), r)

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
