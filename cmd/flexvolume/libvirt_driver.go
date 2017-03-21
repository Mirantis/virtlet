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

func PrintResult(status string, message string, device string) {
	data := map[string]string{
		"status":  status,
		"message": message,
	}
	if device != "" {
		data["device"] = device
	}
	result, _ := json.Marshal(data)
	fmt.Printf("%s\n", result)
}

func writeToFile(filePath, stringToWrite string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(stringToWrite)
	return err
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
	if err := writeToFile(target+"/secret.xml", fmt.Sprintf(secretXML, uuid, opts.User)); err != nil {
		return err
	}

	// Will be removed right after creating appropriate secret in libvirt
	if err := writeToFile(target+"/key", opts.Secret); err != nil {
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
	if err := writeToFile(target+"/disk.xml", fmt.Sprintf(diskXML, opts.User, uuid, opts.Pool, opts.Volume, pairIPPort[0], pairIPPort[1], "%s")); err != nil {
		return err
	}
	return nil
}

func Init() {
	PrintResult("Success", "No initialization logic needed", "")
}

func Attach(jsonArgStr string) {
	PrintResult("Success", "No logic needed", "")
}

func Mount(target string, device string, jsonArgStr string) {
	var jsonArgs AttachOpts
	_ = json.Unmarshal([]byte(jsonArgStr), &jsonArgs)
	if err := os.MkdirAll(target, 0700); err != nil {
		PrintResult("Failure", err.Error(), "")
		return
	}
	if err := AddVolumeDefinitions(target, jsonArgs); err != nil {
		PrintResult("Failure", err.Error(), "")
		return
	}
	PrintResult("Success", "Volume mounted", "")
}

func Detach() {
	PrintResult("Success", "No detachment logic needed", "")
}

func Unmount(path string) {
	if err := os.RemoveAll(path); err != nil {
		PrintResult("Failure", err.Error(), "")
		return
	}

	PrintResult("Success", "Volume unmounted", "")
}

func main() {
	switch os.Args[1] {
	case "init":
		Init()
	case "attach":
		Attach(os.Args[2])
	case "detach":
		Detach()
	case "mount":
		Mount(os.Args[2], os.Args[3], os.Args[4])
	case "unmount":
		Unmount(os.Args[2])
	default:
		PrintResult("Failed", "Invalid command", "")
	}
}
