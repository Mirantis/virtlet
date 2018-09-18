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
	// DomainStateNoState means "no state", i.e. that the domain state is undefined
	DomainStateNoState DomainState = iota
	// DomainStateRunning means that the domain is running
	DomainStateRunning
	// DomainStateBlocked means that the domain is blocked on resource
	DomainStateBlocked
	// DomainStatePaused means that the domain is paused by user
	DomainStatePaused
	// DomainStateShutdown means that the domain is being shut down
	DomainStateShutdown
	// DomainStateCrashed means that the domain is crashed
	DomainStateCrashed
	// DomainStatePMSuspended means that the domain is suspended
	DomainStatePMSuspended
	// DomainStateShutoff means that the domain is shut off
	DomainStateShutoff
)

// DomainState represents a state of a domain
type DomainState int

// ErrDomainNotFound error is returned by DomainConnection's
// Lookup*() methods when the domain in question cannot be found
var ErrDomainNotFound = errors.New("domain not found")

// ErrSecretNotFound error is returned by DomainConnection's
// Lookup*() methods when the domain in question cannot be found
var ErrSecretNotFound = errors.New("secret not found")

// DomainConnection provides operations on domains that correspond to VMs
type DomainConnection interface {
	// Define creates and returns a new domain based on the specified definition
	DefineDomain(def *libvirtxml.Domain) (Domain, error)
	// ListDomains lists all the domains available on the system
	ListDomains() ([]Domain, error)
	// LookupByName tries to locate the domain by name. In case if the
	// domain cannot be found but no other error occurred, it returns
	// ErrDomainNotFound
	LookupDomainByName(name string) (Domain, error)
	// LookupDomainByUUIDString tries to locate the domain by its UUID. In case if the
	// domain cannot be found but no other error occurred, it returns
	// ErrDomainNotFound
	LookupDomainByUUIDString(uuid string) (Domain, error)
	// DefineSecret defines a Secret with the specified value
	DefineSecret(def *libvirtxml.Secret) (Secret, error)
	// LookupSecretByUUIDString tries to locate the secret by its UUID. In case if the
	// secret cannot be found but no other error occurred, it returns
	// ErrSecretNotFound
	LookupSecretByUUIDString(uuid string) (Secret, error)
	// LookupSecretByUsageName tries to locate the secret by its Usage name. In case if the
	// secret cannot be found but no other error occurred, it returns
	// ErrSecretNotFound
	LookupSecretByUsageName(usageType string, usageName string) (Secret, error)
}

// Secret represents a secret that's used by the domain
type Secret interface {
	// SetValue sets the value of the secret
	SetValue(value []byte) error
	// Remove removes the secret
	Remove() error
}

// Domain represents a domain which corresponds to a VM
type Domain interface {
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
	// UUIDString returns UUID string for this domain
	UUIDString() (string, error)
	// Name returns the name of this domain
	Name() (string, error)
	// XML retrieves xml definition of the domain
	XML() (*libvirtxml.Domain, error)
	// GetRSS returns RSS used by VM in bytes
	GetRSS() (uint64, error)
	// GetCPUTime returns cpu time used by VM in nanoseconds per core
	GetCPUTime() (uint64, error)
}
