/*
Copyright 2016 Mirantis

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

package integration

import (
	"fmt"
	"log"
	"os"
	"time"

	virtletutils "github.com/Mirantis/virtlet/pkg/utils"
	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

const (
	maxTime    = 60
	libvirtUri = "qemu+tcp://localhost/system"
)

func waitForSocket(filepath string) error {
	for i := 0; i < maxTime; i++ {
		time.Sleep(1 * time.Second)
		if _, err := os.Stat(filepath); err == nil {
			return nil
		}
	}

	return fmt.Errorf("Socket %s doesn't exist", filepath)
}

func defineDummyDomain() error {
	conn, err := libvirt.NewConnect(libvirtUri)
	if err != nil {
		return err
	}

	uuid, err := virtletutils.NewUuid()
	if err != nil {
		log.Panicf("NewUuid(): %v", err)
	}

	domain := &libvirtxml.Domain{
		Name: "dummy-" + uuid,
		Type: "qemu",
		OS: &libvirtxml.DomainOS{
			Type: &libvirtxml.DomainOSType{Type: "hvm"},
		},
		Memory: &libvirtxml.DomainMemory{Value: 8192, Unit: "KiB"},
	}

	domainXML, err := domain.Marshal()
	if err != nil {
		log.Panicf("XML marshaling: %v", err)
	}

	_, err = conn.DomainDefineXML(domainXML)
	return err
}
