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
	"fmt"

	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt"
)

type libvirtDomainConnection struct {
	conn libvirtConnection
}

var _ virt.DomainConnection = &libvirtDomainConnection{}

func newLibvirtDomainConnection(conn libvirtConnection) *libvirtDomainConnection {
	return &libvirtDomainConnection{conn: conn}
}

func (dc *libvirtDomainConnection) DefineDomain(def *libvirtxml.Domain) (virt.Domain, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Defining domain:\n%s", xml)
	d, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.DomainDefineXML(xml)
	})
	if err != nil {
		return nil, err
	}
	return &libvirtDomain{d.(*libvirt.Domain)}, nil
}

func (dc *libvirtDomainConnection) ListDomains() ([]virt.Domain, error) {
	domains, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.ListAllDomains(0)
	})
	if err != nil {
		return nil, err
	}
	var r []virt.Domain
	for _, d := range domains.([]libvirt.Domain) {
		// need to make a copy here
		curDomain := d
		r = append(r, &libvirtDomain{&curDomain})
	}
	return r, nil
}

func (dc *libvirtDomainConnection) LookupDomainByName(name string) (virt.Domain, error) {
	d, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupDomainByName(name)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return nil, virt.ErrDomainNotFound
		}
		return nil, err
	}
	return &libvirtDomain{d.(*libvirt.Domain)}, nil
}

func (dc *libvirtDomainConnection) LookupDomainByUUIDString(uuid string) (virt.Domain, error) {
	d, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupDomainByUUIDString(uuid)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return nil, virt.ErrDomainNotFound
		}
		return nil, err
	}
	return &libvirtDomain{d.(*libvirt.Domain)}, nil
}

func (dc *libvirtDomainConnection) DefineSecret(def *libvirtxml.Secret) (virt.Secret, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	secret, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.SecretDefineXML(xml, 0)
	})
	if err != nil {
		return nil, err
	}
	return &libvirtSecret{secret.(*libvirt.Secret)}, nil
}

func (dc *libvirtDomainConnection) LookupSecretByUUIDString(uuid string) (virt.Secret, error) {
	secret, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupSecretByUUIDString(uuid)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_SECRET {
			return nil, virt.ErrSecretNotFound
		}
		return nil, err
	}
	return &libvirtSecret{secret.(*libvirt.Secret)}, nil
}

func (dc *libvirtDomainConnection) LookupSecretByUsageName(usageType string, usageName string) (virt.Secret, error) {

	if usageType != "ceph" {
		return nil, fmt.Errorf("unsupported type %q for secret with usage name: %q", usageType, usageName)
	}

	secret, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupSecretByUsage(libvirt.SECRET_USAGE_TYPE_CEPH, usageName)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_SECRET {
			return nil, virt.ErrSecretNotFound
		}
		return nil, err
	}
	return &libvirtSecret{secret.(*libvirt.Secret)}, nil
}

type libvirtDomain struct {
	d *libvirt.Domain
}

var _ virt.Domain = &libvirtDomain{}

func (domain *libvirtDomain) Create() error {
	return domain.d.Create()
}

func (domain *libvirtDomain) Destroy() error {
	return domain.d.Destroy()
}

func (domain *libvirtDomain) Undefine() error {
	return domain.d.Undefine()
}

func (domain *libvirtDomain) Shutdown() error {
	return domain.d.Shutdown()
}

func (domain *libvirtDomain) State() (virt.DomainState, error) {
	di, err := domain.d.GetInfo()
	if err != nil {
		return virt.DomainStateNoState, err
	}
	switch di.State {
	case libvirt.DOMAIN_NOSTATE:
		return virt.DomainStateNoState, nil
	case libvirt.DOMAIN_RUNNING:
		return virt.DomainStateRunning, nil
	case libvirt.DOMAIN_BLOCKED:
		return virt.DomainStateBlocked, nil
	case libvirt.DOMAIN_PAUSED:
		return virt.DomainStatePaused, nil
	case libvirt.DOMAIN_SHUTDOWN:
		return virt.DomainStateShutdown, nil
	case libvirt.DOMAIN_CRASHED:
		return virt.DomainStateCrashed, nil
	case libvirt.DOMAIN_PMSUSPENDED:
		return virt.DomainStatePMSuspended, nil
	case libvirt.DOMAIN_SHUTOFF:
		return virt.DomainStateShutoff, nil
	default:
		return virt.DomainStateNoState, fmt.Errorf("bad domain state %v", di.State)
	}
}

func (domain *libvirtDomain) UUIDString() (string, error) {
	return domain.d.GetUUIDString()
}

func (domain *libvirtDomain) Name() (string, error) {
	return domain.d.GetName()
}

func (domain *libvirtDomain) XML() (*libvirtxml.Domain, error) {
	desc, err := domain.d.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return nil, err
	}
	var d libvirtxml.Domain
	if err := d.Unmarshal(desc); err != nil {
		return nil, fmt.Errorf("error unmarshalling domain definition: %v", err)
	}
	return &d, nil
}

// GetRSS returns RSS used by VM in bytes
func (domain *libvirtDomain) GetRSS() (uint64, error) {
	stats, err := domain.d.MemoryStats(uint32(libvirt.DOMAIN_MEMORY_STAT_LAST), 0)
	if err != nil {
		return 0, err
	}
	for _, stat := range stats {
		if stat.Tag == int32(libvirt.DOMAIN_MEMORY_STAT_RSS) {
			return stat.Val * 1024, nil
		}
	}
	return 0, fmt.Errorf("rss not found in memory stats")
}

// GetCPUTime returns cpu time used by VM in nanoseconds per core
func (domain *libvirtDomain) GetCPUTime() (uint64, error) {
	// all vcpus as a single value
	stats, err := domain.d.GetCPUStats(-1, 1, 0)
	if err != nil {
		return 0, err
	}
	if len(stats) != 1 {
		return 0, fmt.Errorf("domain.GetCPUStats returned %d values while single one was expected", len(stats))
	}
	if !stats[0].VcpuTimeSet {
		return 0, fmt.Errorf("domain.VcpuTime not found in memory stats")
	}
	return stats[0].VcpuTime, nil
}

type libvirtSecret struct {
	s *libvirt.Secret
}

func (secret *libvirtSecret) SetValue(value []byte) error {
	return secret.s.SetValue(value, 0)
}

func (secret *libvirtSecret) Remove() error {
	return secret.s.Undefine()
}
