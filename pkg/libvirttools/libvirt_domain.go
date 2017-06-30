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

type LibvirtDomainConnection struct {
	conn *libvirt.Connect
}

var _ virt.VirtDomainConnection = &LibvirtDomainConnection{}

func newLibvirtDomainConnection(conn *libvirt.Connect) *LibvirtDomainConnection {
	return &LibvirtDomainConnection{conn: conn}
}

func (dc *LibvirtDomainConnection) DefineDomain(def *libvirtxml.Domain) (virt.VirtDomain, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Defining domain:\n%s", xml)
	d, err := dc.conn.DomainDefineXML(xml)
	if err != nil {
		return nil, err
	}
	return &LibvirtDomain{d}, nil
}

func (dc *LibvirtDomainConnection) ListDomains() ([]virt.VirtDomain, error) {
	domains, err := dc.conn.ListAllDomains(0)
	if err != nil {
		return nil, err
	}
	r := make([]virt.VirtDomain, len(domains))
	for n, d := range domains {
		// need to make a copy here
		curDomain := d
		r[n] = &LibvirtDomain{&curDomain}
	}
	return r, nil
}

func (dc *LibvirtDomainConnection) LookupDomainByName(name string) (virt.VirtDomain, error) {
	d, err := dc.conn.LookupDomainByName(name)
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return nil, virt.ErrDomainNotFound
		}
		return nil, err
	}
	return &LibvirtDomain{d}, nil
}

func (dc *LibvirtDomainConnection) LookupDomainByUUIDString(uuid string) (virt.VirtDomain, error) {
	d, err := dc.conn.LookupDomainByUUIDString(uuid)
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return nil, virt.ErrDomainNotFound
		}
		return nil, err
	}
	return &LibvirtDomain{d}, nil
}

func (dc *LibvirtDomainConnection) DefineSecret(def *libvirtxml.Secret) (virt.VirtSecret, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	secret, err := dc.conn.SecretDefineXML(xml, 0)
	if err != nil {
		return nil, err
	}
	return &LibvirtSecret{secret}, nil
}

func (dc *LibvirtDomainConnection) LookupSecretByUUIDString(uuid string) (virt.VirtSecret, error) {
	secret, err := dc.conn.LookupSecretByUUIDString(uuid)
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_SECRET {
			return nil, virt.ErrSecretNotFound
		}
		return nil, err
	}
	return &LibvirtSecret{secret}, nil
}

func (dc *LibvirtDomainConnection) LookupSecretByUsageName(usageType string, usageName string) (virt.VirtSecret, error) {

	if usageType != "ceph" {
		return nil, fmt.Errorf("unsupported type %q for secret with usage name: %q", usageType, usageName)
	}

	secret, err := dc.conn.LookupSecretByUsage(libvirt.SECRET_USAGE_TYPE_CEPH, usageName)
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_SECRET {
			return nil, virt.ErrSecretNotFound
		}
		return nil, err
	}
	return &LibvirtSecret{secret}, nil
}

type LibvirtDomain struct {
	d *libvirt.Domain
}

var _ virt.VirtDomain = &LibvirtDomain{}

func (domain *LibvirtDomain) Create() error {
	return domain.d.Create()
}

func (domain *LibvirtDomain) Destroy() error {
	return domain.d.Destroy()
}

func (domain *LibvirtDomain) Undefine() error {
	return domain.d.Undefine()
}

func (domain *LibvirtDomain) Shutdown() error {
	return domain.d.Shutdown()
}

func (domain *LibvirtDomain) State() (virt.DomainState, error) {
	di, err := domain.d.GetInfo()
	if err != nil {
		return virt.DOMAIN_NOSTATE, err
	}
	switch di.State {
	case libvirt.DOMAIN_NOSTATE:
		return virt.DOMAIN_NOSTATE, nil
	case libvirt.DOMAIN_RUNNING:
		return virt.DOMAIN_RUNNING, nil
	case libvirt.DOMAIN_BLOCKED:
		return virt.DOMAIN_BLOCKED, nil
	case libvirt.DOMAIN_PAUSED:
		return virt.DOMAIN_PAUSED, nil
	case libvirt.DOMAIN_SHUTDOWN:
		return virt.DOMAIN_SHUTDOWN, nil
	case libvirt.DOMAIN_CRASHED:
		return virt.DOMAIN_CRASHED, nil
	case libvirt.DOMAIN_PMSUSPENDED:
		return virt.DOMAIN_PMSUSPENDED, nil
	case libvirt.DOMAIN_SHUTOFF:
		return virt.DOMAIN_SHUTOFF, nil
	default:
		return virt.DOMAIN_NOSTATE, fmt.Errorf("bad domain state %v", di.State)
	}
}

func (domain *LibvirtDomain) UUIDString() (string, error) {
	return domain.d.GetUUIDString()
}

type LibvirtSecret struct {
	s *libvirt.Secret
}

func (secret *LibvirtSecret) SetValue(value []byte) error {
	return secret.s.SetValue(value, 0)
}

func (secret *LibvirtSecret) Remove() error {
	return secret.s.Undefine()
}