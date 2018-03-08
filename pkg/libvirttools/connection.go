/*
Copyright 2016-2017 Mirantis

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

// Connection combines accessors for methods which operated on libvirt storage
// and domains.
type Connection struct {
	*libvirtDomainConnection
	*libvirtStorageConnection
}

// NewConnection uses uri to construct connection to libvirt used later by
// both storage and domains manipulators.
func NewConnection(uri string) (*Connection, error) {
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return nil, err
	}
	return &Connection{
		libvirtDomainConnection:  newLibvirtDomainConnection(conn),
		libvirtStorageConnection: newLibvirtStorageConnection(conn),
	}, nil
}
