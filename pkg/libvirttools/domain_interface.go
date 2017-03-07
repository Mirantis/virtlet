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

package libvirttools

import (
	libvirt "github.com/libvirt/libvirt-go"
)

// NOTE: Implementation of GetLastError in libvirt-go resets error at the end.
// All DomainOperations methods invoke GetLastError, so
// repeated call returns no error and doesn't make sense.

type DomainOperations interface {
	Create(domain *libvirt.Domain) error
	DefineFromXML(xmlConfig string) (*libvirt.Domain, error)
	Destroy(domain *libvirt.Domain) error
	Undefine(domain *libvirt.Domain) error
	Shutdown(domain *libvirt.Domain) error
	ListAll() ([]libvirt.Domain, error)
	LookupByName(name string) (*libvirt.Domain, error)
	LookupByUUIDString(uuid string) (*libvirt.Domain, error)
	GetDomainInfo(domain *libvirt.Domain) (*libvirt.DomainInfo, error)
	GetUUIDString(domain *libvirt.Domain) (string, error)
}

type LibvirtDomainOperations struct {
	conn *libvirt.Connect
}

func NewLibvirtDomainOperations(conn *libvirt.Connect) DomainOperations {
	return LibvirtDomainOperations{conn: conn}
}

func (l LibvirtDomainOperations) Create(domain *libvirt.Domain) error {
	return domain.Create()
}

func (l LibvirtDomainOperations) Destroy(domain *libvirt.Domain) error {
	return domain.Destroy()
}

func (l LibvirtDomainOperations) Undefine(domain *libvirt.Domain) error {
	return domain.Undefine()
}

func (l LibvirtDomainOperations) DefineFromXML(xmlConfig string) (*libvirt.Domain, error) {
	return l.conn.DomainDefineXML(xmlConfig)
}

func (l LibvirtDomainOperations) Shutdown(domain *libvirt.Domain) error {
	return domain.Shutdown()
}

func (l LibvirtDomainOperations) ListAll() ([]libvirt.Domain, error) {
	return l.conn.ListAllDomains(0)
}

func (l LibvirtDomainOperations) LookupByName(name string) (*libvirt.Domain, error) {
	return l.conn.LookupDomainByName(name)
}

func (l LibvirtDomainOperations) LookupByUUIDString(uuid string) (*libvirt.Domain, error) {
	return l.conn.LookupDomainByUUIDString(uuid)
}

func (l LibvirtDomainOperations) GetDomainInfo(domain *libvirt.Domain) (*libvirt.DomainInfo, error) {
	return domain.GetInfo()
}

func (l LibvirtDomainOperations) GetUUIDString(domain *libvirt.Domain) (string, error) {
	return domain.GetUUIDString()
}
