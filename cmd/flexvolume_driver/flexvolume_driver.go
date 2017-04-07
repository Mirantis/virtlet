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

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	uuid "github.com/nu7hatch/gouuid"
)

type AttachOpts struct {
	FsType    string `json:"kubernetes.io/fsType"`
	Readwrite string `json:"kubernetes.io/readwrite"`
	Monitor   string `json:"monitor"`
	Pool      string `json:"pool"`
	Volume    string `json:"volume"`
	Secret    string `json:"secret"`
	User      string `json:user`
	Protocol  string `json:protocol`
}

func PrintResult(status string, message string) {
	data := map[string]string{
		"status":  status,
		"message": message,
	}
	result, err := json.Marshal(data)
	if err != nil {
		result = []byte(fmt.Sprintf("{\"status\": \"Failure\", \"message\": \"Json marshal error: %s\"}", err.Error()))
	}
	fmt.Printf("%s\n", result)
}

func NewUuid() (string, error) {
	u, err := uuid.NewV4()
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func AddVolumeDefinitions(target string, opts AttachOpts) error {
	uuid, err := NewUuid()
	if err != nil {
		return err
	}

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
		return fmt.Errorf("Invalid format of ceph monitor setting: %s. Expected in form ip:port", opts.Monitor)
	}
	// Note: target dev name wiil be specified later by virtlet diring combining domain xml definition
	if err := ioutil.WriteFile(target+"/disk.xml", []byte(fmt.Sprintf(diskXML, opts.User, uuid, opts.Pool, opts.Volume, pairIPPort[0], pairIPPort[1], "%s")), 0644); err != nil {
		return err
	}
	return nil
}

func Init(args []string) {
	PrintResult("Success", "No initialization logic needed")
}

func Attach(args []string) {
	PrintResult("Success", "No logic needed")
}

func Mount(args []string) {
	target := args[0]
	// device := args[1]
	jsonArgStr := args[2]
	var jsonArgs AttachOpts
	_ = json.Unmarshal([]byte(jsonArgStr), &jsonArgs)
	if err := os.MkdirAll(target, 0700); err != nil {
		PrintResult("Failure", err.Error())
		return
	}
	if err := AddVolumeDefinitions(target, jsonArgs); err != nil {
		PrintResult("Failure", err.Error())
		return
	}
	PrintResult("Success", "Volume mounted")
}

func Detach(args []string) {
	PrintResult("Success", "No detachment logic needed")
}

func Unmount(args []string) {
	path := args[0]
	if err := os.RemoveAll(path); err != nil {
		PrintResult("Failure", err.Error())
		return
	}

	PrintResult("Success", "Volume unmounted")
}

type driverFunc func([]string)

type cmdInfo struct {
	numArgs     int
	processFunc driverFunc
}

var cmdArgsMatrix = map[string]cmdInfo{
	"init":    cmdInfo{0, Init},
	"attach":  cmdInfo{1, Attach},
	"detach":  cmdInfo{0, Detach},
	"mount":   cmdInfo{3, Mount},
	"unmount": cmdInfo{1, Unmount},
}

func main() {
	if len(os.Args) == 1 {
		PrintResult("Failed", "No command name to execute was provided.")
		return
	}
	funcName := os.Args[1]
	numArgs := len(os.Args) - 2

	if cmdInfo, found := cmdArgsMatrix[funcName]; found {
		if cmdInfo.numArgs == numArgs {
			funcArgs := []string{}
			if numArgs > 0 {
				funcArgs = os.Args[2:]
			}
			cmdInfo.processFunc(funcArgs)
		} else {
			PrintResult("Failed", fmt.Sprintf("Unexpected number of args %d (expected %d) for func '%s'", numArgs, cmdInfo.numArgs, funcName))
		}
	} else {
		PrintResult("Failed", "Unknown func name "+os.Args[1])
	}
}
