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

var pathReplacements = [][2]string{
	{
		"/__config__/",
		"/var/lib/virtlet/config/",
	},
	{
		"/__pods__/",
		"/var/lib/kubelet/pods/",
	},
}

func mustMarshal(d libvirtxml.Document) string {
	s, err := d.Marshal()
	if err != nil {
		log.Panicf("Error marshaling libvirt doc: %v", err)
	}
	return s
}

// FakeDomainConnection is a fake implementation of DomainConnection interface.
type FakeDomainConnection struct {
	rec                     testutils.Recorder
	domains                 map[string]*FakeDomain
	domainsByUuid           map[string]*FakeDomain
	secretsByUsageName      map[string]*FakeSecret
	ignoreShutdown          bool
	useNonVolatileDomainDef bool
}

var _ virt.DomainConnection = &FakeDomainConnection{}

// NewFakeDomainConnection creates a new FakeDomainConnection using
// the specified Recorder to record any changes.
func NewFakeDomainConnection(rec testutils.Recorder) *FakeDomainConnection {
	if rec == nil {
		rec = testutils.NullRecorder
	}
	return &FakeDomainConnection{
		rec:                rec,
		domains:            make(map[string]*FakeDomain),
		domainsByUuid:      make(map[string]*FakeDomain),
		secretsByUsageName: make(map[string]*FakeSecret),
	}
}

// UseNonVolatileDomainDef instructs the domains to fix volatile paths
// in the domain definitions returned by domains' XML() method.
func (dc *FakeDomainConnection) UseNonVolatileDomainDef() {
	dc.useNonVolatileDomainDef = true
}

// SetIgnoreShutdown implements SetIgnoreShutdown method of DomainConnection interface.
func (dc *FakeDomainConnection) SetIgnoreShutdown(ignoreShutdown bool) {
	dc.ignoreShutdown = ignoreShutdown
}

func (dc *FakeDomainConnection) removeDomain(d *FakeDomain) {
	if _, found := dc.domains[d.def.Name]; !found {
		log.Panicf("domain %q not found", d.def.Name)
	}
	delete(dc.domains, d.def.Name)
	if _, found := dc.domainsByUuid[d.def.UUID]; !found {
		log.Panicf("domain uuid %q not found (name %q)", d.def.UUID, d.def.Name)
	}
	delete(dc.domainsByUuid, d.def.UUID)
}

func (dc *FakeDomainConnection) removeSecret(s *FakeSecret) {
	if _, found := dc.secretsByUsageName[s.usageName]; !found {
		log.Panicf("secret %q not found", s.usageName)
	}
	delete(dc.secretsByUsageName, s.usageName)
}

// DefineDomain implements DefineDomain method of DomainConnection interface.
func (dc *FakeDomainConnection) DefineDomain(def *libvirtxml.Domain) (virt.Domain, error) {
	def = copyDomain(def)
	addPciRoot(def)
	assignFakePCIAddressesToControllers(def)
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
	d := newFakeDomain(dc, def)
	dc.domains[def.Name] = d
	dc.domainsByUuid[def.UUID] = d

	updatedDef := copyDomain(def)
	removeVolatilePathsFromDomainDef(updatedDef)
	dc.rec.Rec("DefineDomain", mustMarshal(updatedDef))
	return d, nil
}

// ListDomains implements ListDomains method of DomainConnection interface.
func (dc *FakeDomainConnection) ListDomains() ([]virt.Domain, error) {
	r := make([]virt.Domain, len(dc.domains))
	names := make([]string, 0, len(dc.domains))
	for name := range dc.domains {
		names = append(names, name)
	}
	sort.Strings(names)
	for n, name := range names {
		r[n] = dc.domains[name]
	}
	dc.rec.Rec("ListDomains", names)
	return r, nil
}

// LookupDomainByName implements LookupDomainByName method of DomainConnection interface.
func (dc *FakeDomainConnection) LookupDomainByName(name string) (virt.Domain, error) {
	if d, found := dc.domains[name]; found {
		return d, nil
	}
	return nil, virt.ErrDomainNotFound
}

// LookupDomainByUUIDString implements LookupDomainByUUIDString method of DomainConnection interface.
func (dc *FakeDomainConnection) LookupDomainByUUIDString(uuid string) (virt.Domain, error) {
	if d, found := dc.domainsByUuid[uuid]; found {
		return d, nil
	}
	return nil, virt.ErrDomainNotFound
}

// DefineSecret implements DefineSecret method of DomainConnection interface.
func (dc *FakeDomainConnection) DefineSecret(def *libvirtxml.Secret) (virt.Secret, error) {
	if def.UUID == "" {
		return nil, fmt.Errorf("the secret has empty uuid")
	}
	if def.Usage.Name == "" {
		return nil, fmt.Errorf("the secret has empty Usage name")
	}
	// clear secret uuid as it's generated randomly
	def.UUID = ""
	dc.rec.Rec("DefineSecret", mustMarshal(def))

	s := newFakeSecret(dc, def.Usage.Name)
	dc.secretsByUsageName[def.Usage.Name] = s
	return s, nil
}

// LookupSecretByUUIDString implements LookupSecretByUUIDString method of DomainConnection interface.
func (dc *FakeDomainConnection) LookupSecretByUUIDString(uuid string) (virt.Secret, error) {
	return nil, virt.ErrSecretNotFound
}

// LookupSecretByUsageName implements LookupSecretByUsageName method of DomainConnection interface.
func (dc *FakeDomainConnection) LookupSecretByUsageName(usageType string, usageName string) (virt.Secret, error) {
	if d, found := dc.secretsByUsageName[usageName]; found {
		return d, nil
	}
	return nil, virt.ErrSecretNotFound
}

// FakeDomain is a fake implementation of Domain interface.
type FakeDomain struct {
	rec     testutils.Recorder
	dc      *FakeDomainConnection
	removed bool
	created bool
	state   virt.DomainState
	def     *libvirtxml.Domain
}

var _ virt.Domain = &FakeDomain{}

func newFakeDomain(dc *FakeDomainConnection, def *libvirtxml.Domain) *FakeDomain {
	return &FakeDomain{
		rec:   testutils.NewChildRecorder(dc.rec, def.Name),
		dc:    dc,
		state: virt.DomainStateShutoff,
		def:   def,
	}
}

// Create implements Create method of Domain interface.
func (d *FakeDomain) Create() error {
	d.rec.Rec("Create", nil)
	if d.def.Devices != nil {
		for _, disk := range d.def.Devices.Disks {
			if disk.Source == nil || disk.Source.File == nil {
				continue
			}
			origPath := disk.Source.File.File
			if filepath.Ext(origPath) == ".iso" || strings.HasPrefix(filepath.Base(origPath), "config-iso") {
				m, err := testutils.IsoToMap(origPath)
				if err != nil {
					return fmt.Errorf("bad iso image: %q", origPath)
				}
				d.rec.Rec("iso image", m)
			}
		}
	}
	if d.removed {
		return fmt.Errorf("Create() called on a removed (undefined) domain %q", d.def.Name)
	}
	if d.created {
		return fmt.Errorf("trying to re-create domain %q", d.def.Name)
	}
	if d.state != virt.DomainStateShutoff {
		return fmt.Errorf("invalid domain state %d", d.state)
	}
	d.created = true
	d.state = virt.DomainStateRunning
	return nil
}

// Destroy implements Destroy method of Domain interface.
func (d *FakeDomain) Destroy() error {
	d.rec.Rec("Destroy", nil)
	if d.removed {
		return fmt.Errorf("Destroy() called on a removed (undefined) domain %q", d.def.Name)
	}
	d.state = virt.DomainStateShutoff
	return nil
}

// Undefine implements Undefine method of Domain interface.
func (d *FakeDomain) Undefine() error {
	d.rec.Rec("Undefine", nil)
	if d.removed {
		return fmt.Errorf("Undefine(): domain %q already removed", d.def.Name)
	}
	d.removed = true
	d.dc.removeDomain(d)
	return nil
}

// Shutdown implements Shutdown method of Domain interface.
func (d *FakeDomain) Shutdown() error {
	if d.dc.ignoreShutdown {
		d.rec.Rec("Shutdown", map[string]interface{}{"ignored": true})
	} else {
		d.rec.Rec("Shutdown", nil)
	}
	if d.removed {
		return fmt.Errorf("Shutdown() called on a removed (undefined) domain %q", d.def.Name)
	}
	if !d.dc.ignoreShutdown {
		// TODO: need to test DomainStateShutdown stage too
		d.state = virt.DomainStateShutoff
	}
	return nil
}

// State implements State method of Domain interface.
func (d *FakeDomain) State() (virt.DomainState, error) {
	if d.removed {
		return virt.DomainStateNoState, fmt.Errorf("State() called on a removed (undefined) domain %q", d.def.Name)
	}
	return d.state, nil
}

// UUIDString implements UUIDString method of Domain interface.
func (d *FakeDomain) UUIDString() (string, error) {
	if d.removed {
		return "", fmt.Errorf("UUIDString() called on a removed (undefined) domain %q", d.def.Name)
	}
	return d.def.UUID, nil
}

// Name implements Name method of Domain interface.
func (d *FakeDomain) Name() (string, error) {
	return d.def.Name, nil
}

// XML implements XML method of Domain interface.
func (d *FakeDomain) XML() (*libvirtxml.Domain, error) {
	if d.dc.useNonVolatileDomainDef {
		def := copyDomain(d.def)
		removeVolatilePathsFromDomainDef(def)
		return def, nil
	}
	return d.def, nil
}

// FakeSecret is a fake implementation of Secret interace.
type FakeSecret struct {
	rec       testutils.Recorder
	dc        *FakeDomainConnection
	usageName string
}

var _ virt.Secret = &FakeSecret{}

func newFakeSecret(dc *FakeDomainConnection, usageName string) *FakeSecret {
	return &FakeSecret{
		rec:       testutils.NewChildRecorder(dc.rec, "secret "+usageName),
		dc:        dc,
		usageName: usageName,
	}
}

// SetValue implements SetValue method of Secret interface.
func (s *FakeSecret) SetValue(value []byte) error {
	s.rec.Rec("SetValue", fmt.Sprintf("% x", value))
	return nil
}

// Remove implements Remove method of Secret interface.
func (s *FakeSecret) Remove() error {
	s.rec.Rec("Remove", nil)
	s.dc.removeSecret(s)
	return nil
}

func copyDomain(def *libvirtxml.Domain) *libvirtxml.Domain {
	s, err := def.Marshal()
	if err != nil {
		log.Panicf("failed to marshal libvirt domain: %v", err)
	}
	var copy libvirtxml.Domain
	if err := copy.Unmarshal(s); err != nil {
		log.Panicf("failed to unmarshal libvirt domain: %v", err)
	}
	return &copy
}

func addPciRoot(def *libvirtxml.Domain) {
	if def.Devices == nil {
		def.Devices = &libvirtxml.DomainDeviceList{}
	}
	for _, c := range def.Devices.Controllers {
		if c.Type == "pci" {
			return
		}
	}
	def.Devices.Controllers = append(def.Devices.Controllers, libvirtxml.DomainController{
		Type:  "pci",
		Model: "pci-root",
	})
}

func assignFakePCIAddressesToControllers(def *libvirtxml.Domain) {
	if def.Devices == nil {
		return
	}
	domain := uint(0)
	bus := uint(0)
	function := uint(0)
	for n, c := range def.Devices.Controllers {
		if c.Type == "pci" || c.Address != nil {
			continue
		}
		slot := uint(n + 1)
		// note that c is not a pointer
		def.Devices.Controllers[n].Address = &libvirtxml.DomainAddress{
			PCI: &libvirtxml.DomainAddressPCI{
				Domain:   &domain,
				Bus:      &bus,
				Slot:     &slot,
				Function: &function,
			},
		}
	}
}

func removeVolatilePathsFromDomainDef(def *libvirtxml.Domain) {
	if def.Devices == nil {
		return
	}

	for _, disk := range def.Devices.Disks {
		var toUpdate *string
		switch {
		case disk.Source == nil:
			continue
		case disk.Source.File != nil:
			toUpdate = &disk.Source.File.File
		case disk.Source.Block != nil:
			toUpdate = &disk.Source.Block.Dev
		default:
			continue
		}
		for _, pr := range pathReplacements {
			p := strings.Index(*toUpdate, pr[0])
			if p >= 0 {
				*toUpdate = pr[1] + (*toUpdate)[p+len(pr[0]):]
			}
		}
	}
}
