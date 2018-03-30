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
	"time"

	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
)

const (
	libvirtReconnectInterval = 1 * time.Second
	libvirtReconnectAttempts = 120
)

type libvirtCall func(c *libvirt.Connect) (interface{}, error)

type libvirtConnection interface {
	invoke(call libvirtCall) (interface{}, error)
}

// Connection combines accessors for methods which operated on libvirt storage
// and domains.
type Connection struct {
	uri  string
	conn *libvirt.Connect
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
	r := &Connection{
		uri:  uri,
		conn: conn,
	}
	r.libvirtDomainConnection = newLibvirtDomainConnection(r)
	r.libvirtStorageConnection = newLibvirtStorageConnection(r)
	return r, nil
}

func (c *Connection) connect() error {
	var err error
	for i := 0; i < libvirtReconnectAttempts; i++ {
		if i > 0 {
			time.Sleep(libvirtReconnectInterval)
		}
		glog.V(1).Infof("Connecting to libvirt at %s", c.uri)
		c.conn, err = libvirt.NewConnect(c.uri)
		if err == nil {
			return nil
		}
		glog.Warningf("Error connecting to libvirt at %s: %v", c.uri, err)
	}
	glog.Warningf("Failed to connect to libvirt at %s after %d attempts", c.uri, libvirtReconnectAttempts)
	return err
}

func (c *Connection) invoke(call libvirtCall) (interface{}, error) {
	for {
		if c.conn == nil {
			if err := c.connect(); err != nil {
				return nil, err
			}
		}

		r, err := call(c.conn)
		switch err := err.(type) {
		case nil:
			return r, nil
		case libvirt.Error:
			if err.Domain == libvirt.FROM_RPC && err.Code == libvirt.ERR_INTERNAL_ERROR {
				c.conn = nil
				continue
			}
			return nil, err
		default:
			return nil, err
		}
	}
}
