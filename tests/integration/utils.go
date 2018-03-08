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

	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/libvirttools"
	virtletutils "github.com/Mirantis/virtlet/pkg/utils"
)

const (
	maxTime    = 60
	libvirtUri = "qemu:///system"
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

func getDomainUUID(podID string) string {
	return virtletutils.NewUUID5(libvirttools.ContainerNsUUID, podID)
}

func defineDummyVolume(poolName, volName string) (func(), error) {
	conn, err := libvirt.NewConnect(libvirtUri)
	if err != nil {
		return nil, err
	}

	pool, err := conn.LookupStoragePoolByName(poolName)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup pool '%s': %v", poolName, err)
	}

	volume := &libvirtxml.StorageVolume{
		Name:     volName,
		Capacity: &libvirtxml.StorageVolumeSize{Unit: "bytes", Value: 1},
	}

	volXML, err := volume.Marshal()
	if err != nil {
		log.Panicf("XML marshaling: %v", err)
	}

	vol, err := pool.StorageVolCreateXML(volXML, 0)
	return func() {
		if vol == nil {
			return
		}
		if err := vol.Delete(0); err != nil {
			log.Printf("WARNING: failed to clean up volume %q in pool %q: %v", volName, poolName, err)
		} else {
			vol = nil
		}
	}, err
}

func undefDomain(domainName string) error {
	conn, err := libvirt.NewConnect(libvirtUri)
	if err != nil {
		return err
	}
	domain, err := conn.LookupDomainByName(domainName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain by name %q: %v", domainName, err)
	}
	if err := domain.Undefine(); err != nil {
		return fmt.Errorf("failed to lookup domain by name %q: %v", domainName, err)
	}
	return nil
}

func defineDummyDomain() error {
	return defineDummyDomainWithName("")
}

func defineDummyDomainWithName(domainName string) error {
	if domainName == "" {
		domainName = "dummy-" + virtletutils.NewUUID()
	}

	conn, err := libvirt.NewConnect(libvirtUri)
	if err != nil {
		return err
	}

	domain := &libvirtxml.Domain{
		Name: domainName,
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
