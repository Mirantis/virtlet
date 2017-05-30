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

package fake

import (
	"fmt"
	"log"
	"sort"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt"
)

type FakeDomainConnection struct {
	domains       map[string]*FakeDomain
	domainsByUuid map[string]*FakeDomain
}

var _ virt.VirtDomainConnection = &FakeDomainConnection{}

func NewFakeDomainConnection() *FakeDomainConnection {
	return &FakeDomainConnection{
		domains:       make(map[string]*FakeDomain),
		domainsByUuid: make(map[string]*FakeDomain),
	}
}

func (dc *FakeDomainConnection) removeDomain(d *FakeDomain) {
	if _, found := dc.domains[d.name]; !found {
		log.Panicf("domain %q not found", d.name)
	}
	delete(dc.domains, d.name)
	if _, found := dc.domainsByUuid[d.uuid]; !found {
		log.Panicf("domain uuid %q not found (name %q)", d.uuid, d.name)
	}
	delete(dc.domainsByUuid, d.uuid)
}

func (dc *FakeDomainConnection) DefineDomain(def *libvirtxml.Domain) (virt.VirtDomain, error) {
	if _, found := dc.domains[def.Name]; found {
		return nil, fmt.Errorf("domain %q already defined", def.Name)
	}
	if def.Name == "" {
		return nil, fmt.Errorf("domain name cannot be empty")
	}
	if def.UUID == "" {
		return nil, fmt.Errorf("domain %q has empty uuid", def.Name)
	}
	d := newFakeDomain(dc, def.Name, def.UUID)
	dc.domains[def.Name] = d
	dc.domainsByUuid[def.UUID] = d
	return d, nil
}

func (dc *FakeDomainConnection) ListDomains() ([]virt.VirtDomain, error) {
	r := make([]virt.VirtDomain, len(dc.domains))
	names := make([]string, 0, len(dc.domains))
	for name, _ := range dc.domains {
		names = append(names, name)
	}
	sort.Strings(names)
	for n, name := range names {
		r[n] = dc.domains[name]
	}
	return r, nil
}

func (dc *FakeDomainConnection) LookupDomainByName(name string) (virt.VirtDomain, error) {
	if d, found := dc.domains[name]; found {
		return d, nil
	}
	return nil, virt.ErrDomainNotFound
}

func (dc *FakeDomainConnection) LookupDomainByUUIDString(uuid string) (virt.VirtDomain, error) {
	if d, found := dc.domainsByUuid[uuid]; found {
		return d, nil
	}
	return nil, virt.ErrDomainNotFound
}

func (dc *FakeDomainConnection) DefineSecret(def *libvirtxml.Secret, value []byte) error {
	// TODO: record
	return nil
}

type FakeDomain struct {
	dc      *FakeDomainConnection
	removed bool
	created bool
	state   virt.DomainState
	name    string
	uuid    string
}

func newFakeDomain(dc *FakeDomainConnection, name, uuid string) *FakeDomain {
	return &FakeDomain{
		dc:    dc,
		state: virt.DOMAIN_SHUTOFF,
		name:  name,
		uuid:  uuid,
	}
}

func (d *FakeDomain) Create() error {
	if d.removed {
		return fmt.Errorf("Create() called on a removed (undefined) domain %q", d.name)
	}
	if d.created {
		return fmt.Errorf("trying to re-create domain %q", d.name)
	}
	if d.state != virt.DOMAIN_SHUTOFF {
		return fmt.Errorf("invalid domain state %d", d.state)
	}
	d.created = true
	d.state = virt.DOMAIN_RUNNING
	return nil
}

func (d *FakeDomain) Destroy() error {
	if d.removed {
		return fmt.Errorf("Destroy() called on a removed (undefined) domain %q", d.name)
	}
	d.state = virt.DOMAIN_SHUTOFF
	return nil
}

func (d *FakeDomain) Undefine() error {
	if d.removed {
		return fmt.Errorf("Undefine(): domain %q already removed", d.name)
	}
	d.removed = true
	d.dc.removeDomain(d)
	return nil
}

func (d *FakeDomain) Shutdown() error {
	if d.removed {
		return fmt.Errorf("Shutdown() called on a removed (undefined) domain %q", d.name)
	}
	// TODO: need to test DOMAIN_SHUTDOWN stage too
	d.state = virt.DOMAIN_SHUTOFF
	return nil
}

func (d *FakeDomain) State() (virt.DomainState, error) {
	if d.removed {
		return virt.DOMAIN_NOSTATE, fmt.Errorf("State() called on a removed (undefined) domain %q", d.name)
	}
	return d.state, nil
}

func (d *FakeDomain) UUIDString() (string, error) {
	if d.removed {
		return "", fmt.Errorf("UUIDString() called on a removed (undefined) domain %q", d.name)
	}
	return d.uuid, nil
}
