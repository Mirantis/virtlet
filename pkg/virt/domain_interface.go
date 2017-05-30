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

package virt

import (
	"errors"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

const (
	// DOMAIN_NOSTATE means "no state", i.e. that the domain state is undefined
	DOMAIN_NOSTATE DomainState = iota
	// DOMAIN_RUNNING means that the domain is running
	DOMAIN_RUNNING
	// DOMAIN_BLOCKED means that the domain is blocked on resource
	DOMAIN_BLOCKED
	// DOMAIN_PAUSED means that the domain is paused by user
	DOMAIN_PAUSED
	// DOMAIN_SHUTDOWN means that the domain is being shut down
	DOMAIN_SHUTDOWN
	// DOMAIN_CRASHED means that the domain is crashed
	DOMAIN_CRASHED
	// DOMAIN_PMSUSPENDED means that the domain is suspended
	DOMAIN_PMSUSPENDED
	// DOMAIN_SHUTOFF means that the domain is shut off
	DOMAIN_SHUTOFF
)

// DomainState represents a state of a domain
type DomainState int

// ErrStoragePoolNotFound error is returned by VirtDomainConnection's
// Lookup*() methods when the domain in question cannot be found
var ErrDomainNotFound = errors.New("domain not found")

// VirtDomainConnection provides operations on domains that correspond to VMs
type VirtDomainConnection interface {
	// Define creates and returns a new domain based on the specified definition
	DefineDomain(def *libvirtxml.Domain) (VirtDomain, error)
	// ListAll lists all the domains available on the system
	ListDomains() ([]VirtDomain, error)
	// LookupByName tries to locate the domain by name. In case if the
	// domain cannot be found but no other error occurred, it returns
	// ErrDomainNotFound
	LookupDomainByName(name string) (VirtDomain, error)
	// LookupByName tries to locate the domain by its UUID. In case if the
	// domain cannot be found but no other error occurred, it returns
	// ErrDomainNotFound
	LookupDomainByUUIDString(uuid string) (VirtDomain, error)
	// DefineSecret defines a Secret with the specified value
	DefineSecret(def *libvirtxml.Secret, value []byte) error
}

// VirtDomain represents a domain which corresponds to a VM
type VirtDomain interface {
	// Create boots the domain
	Create() error
	// Destroy destroys the domain
	Destroy() error
	// Undefine removes the domain so it will no longer be possible
	// to locate it using LookupByName() or LookupByUUIDString()
	Undefine() error
	// Shutdown shuts down the domain
	Shutdown() error
	// State obtains the current state of the domain
	State() (DomainState, error)
	// UUIDString() returns UUID string for this domain
	UUIDString() (string, error)
}
