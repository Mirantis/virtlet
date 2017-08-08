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
	"path/filepath"
	"sort"
	"strings"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type FakeDomainConnection struct {
	rec                Recorder
	domains            map[string]*FakeDomain
	domainsByUuid      map[string]*FakeDomain
	secretsByUsageName map[string]*FakeSecret
	ignoreShutdown     bool
}

var _ virt.VirtDomainConnection = &FakeDomainConnection{}

func NewFakeDomainConnection(rec Recorder) *FakeDomainConnection {
	if rec == nil {
		rec = NullRecorder
	}
	return &FakeDomainConnection{
		rec:                rec,
		domains:            make(map[string]*FakeDomain),
		domainsByUuid:      make(map[string]*FakeDomain),
		secretsByUsageName: make(map[string]*FakeSecret),
	}
}

func (dc *FakeDomainConnection) SetIgnoreShutdown(ignoreShutdown bool) {
	dc.ignoreShutdown = ignoreShutdown
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

func (dc *FakeDomainConnection) removeSecret(s *FakeSecret) {
	if _, found := dc.secretsByUsageName[s.usageName]; !found {
		log.Panicf("secret %q not found", s.usageName)
	}
	delete(dc.secretsByUsageName, s.usageName)
}

func (dc *FakeDomainConnection) DefineDomain(def *libvirtxml.Domain) (virt.VirtDomain, error) {
	if def.Devices != nil {
		for _, disk := range def.Devices.Disks {
			if disk.Type != "file" || disk.Source == nil {
				continue
			}
			origPath := disk.Source.File
			if filepath.Ext(origPath) == ".iso" || strings.HasPrefix(filepath.Base(origPath), "nocloud-iso") {
				m, err := testutils.IsoToMap(origPath)
				if err != nil {
					return nil, fmt.Errorf("bad iso image: %q", origPath)
				}
				dc.rec.Rec("iso image", m)
			}
		}
	}
	dc.rec.Rec("DefineDomain", def)
	// TODO: dump any ISOs mentioned in disks (Type=file) as json
	// Include file name (base) in rec name
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
	dc.rec.Rec("ListDomains", names)
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

func (dc *FakeDomainConnection) DefineSecret(def *libvirtxml.Secret) (virt.VirtSecret, error) {
	if def.UUID == "" {
		return nil, fmt.Errorf("the secret has empty uuid")
	}
	if def.Usage.Name == "" {
		return nil, fmt.Errorf("the secret has empty Usage name")
	}
	// clear secret uuid as it's generated randomly
	def.UUID = ""
	dc.rec.Rec("DefineSecret", def)

	s := newFakeSecret(dc, def.Usage.Name)
	dc.secretsByUsageName[def.Usage.Name] = s
	return s, nil
}

func (dc *FakeDomainConnection) LookupSecretByUUIDString(uuid string) (virt.VirtSecret, error) {
	return nil, virt.ErrSecretNotFound
}

func (dc *FakeDomainConnection) LookupSecretByUsageName(usageType string, usageName string) (virt.VirtSecret, error) {
	if d, found := dc.secretsByUsageName[usageName]; found {
		return d, nil
	}
	return nil, virt.ErrSecretNotFound
}

type FakeDomain struct {
	rec     Recorder
	dc      *FakeDomainConnection
	removed bool
	created bool
	state   virt.DomainState
	name    string
	uuid    string
}

func newFakeDomain(dc *FakeDomainConnection, name, uuid string) *FakeDomain {
	return &FakeDomain{
		rec:   NewChildRecorder(dc.rec, name),
		dc:    dc,
		state: virt.DOMAIN_SHUTOFF,
		name:  name,
		uuid:  uuid,
	}
}

func (d *FakeDomain) Create() error {
	d.rec.Rec("Create", nil)
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
	d.rec.Rec("Destroy", nil)
	if d.removed {
		return fmt.Errorf("Destroy() called on a removed (undefined) domain %q", d.name)
	}
	d.state = virt.DOMAIN_SHUTOFF
	return nil
}

func (d *FakeDomain) Undefine() error {
	d.rec.Rec("Undefine", nil)
	if d.removed {
		return fmt.Errorf("Undefine(): domain %q already removed", d.name)
	}
	d.removed = true
	d.dc.removeDomain(d)
	return nil
}

func (d *FakeDomain) Shutdown() error {
	if d.dc.ignoreShutdown {
		d.rec.Rec("Shutdown", map[string]interface{}{"ignored": true})
	} else {
		d.rec.Rec("Shutdown", nil)
	}
	if d.removed {
		return fmt.Errorf("Shutdown() called on a removed (undefined) domain %q", d.name)
	}
	if !d.dc.ignoreShutdown {
		// TODO: need to test DOMAIN_SHUTDOWN stage too
		d.state = virt.DOMAIN_SHUTOFF
	}
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

func (d *FakeDomain) Name() (string, error) {
	return d.name, nil
}

type FakeSecret struct {
	rec       Recorder
	dc        *FakeDomainConnection
	usageName string
}

func newFakeSecret(dc *FakeDomainConnection, usageName string) *FakeSecret {
	return &FakeSecret{
		rec:       NewChildRecorder(dc.rec, "secret "+usageName),
		dc:        dc,
		usageName: usageName,
	}
}

func (s *FakeSecret) SetValue(value []byte) error {
	s.rec.Rec("SetValue", fmt.Sprintf("% x", value))
	return nil
}

func (s *FakeSecret) Remove() error {
	s.rec.Rec("Remove", nil)
	s.dc.removeSecret(s)
	return nil
}
