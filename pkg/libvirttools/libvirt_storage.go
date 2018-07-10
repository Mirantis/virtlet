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
	"sync"

	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/diskimage"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type libvirtStorageConnection struct {
	// Trying to do several storage-related operations at the same time
	// may cause a race condition.
	// As right now Virtlet uses just one storage pool, a single sync.Mutex
	// is enough for handling the storage.
	sync.Mutex
	conn libvirtConnection
}

var _ virt.StorageConnection = &libvirtStorageConnection{}

func newLibvirtStorageConnection(conn libvirtConnection) *libvirtStorageConnection {
	return &libvirtStorageConnection{conn: conn}
}

func (sc *libvirtStorageConnection) CreateStoragePool(def *libvirtxml.StoragePool) (virt.StoragePool, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Creating storage pool:\n%s", xml)
	p, err := sc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.StoragePoolCreateXML(xml, 0)
	})
	if err != nil {
		return nil, err
	}
	return &libvirtStoragePool{Mutex: &sc.Mutex, conn: sc.conn, p: p.(*libvirt.StoragePool)}, nil
}

func (sc *libvirtStorageConnection) LookupStoragePoolByName(name string) (virt.StoragePool, error) {
	p, err := sc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupStoragePoolByName(name)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_STORAGE_POOL {
			return nil, virt.ErrStoragePoolNotFound
		}
		return nil, err
	}
	return &libvirtStoragePool{Mutex: &sc.Mutex, conn: sc.conn, p: p.(*libvirt.StoragePool)}, nil
}

func (sc *libvirtStorageConnection) ListPools() ([]virt.StoragePool, error) {
	pools, err := sc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.ListAllStoragePools(0)
	})
	if err != nil {
		return nil, err
	}
	var r []virt.StoragePool
	for _, p := range pools.([]libvirt.StoragePool) {
		r = append(r, &libvirtStoragePool{Mutex: &sc.Mutex, conn: sc.conn, p: &p})
	}
	return r, nil
}

type libvirtStoragePool struct {
	*sync.Mutex
	conn libvirtConnection
	p    *libvirt.StoragePool
}

var _ virt.StoragePool = &libvirtStoragePool{}

func (pool *libvirtStoragePool) CreateStorageVol(def *libvirtxml.StorageVolume) (virt.StorageVolume, error) {
	pool.Lock()
	defer pool.Unlock()
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Creating storage volume:\n%s", xml)
	v, err := pool.p.StorageVolCreateXML(xml, 0)
	if err != nil {
		return nil, err
	}
	// libvirt may report qcow2 file size as 'capacity' for
	// qcow2-based volumes for some time after creating them.
	// Here we work around this problem by refreshing the pool
	// which invokes acquiring volume info.
	if err := pool.p.Refresh(0); err != nil {
		return nil, fmt.Errorf("failed to refresh the storage pool: %v", err)
	}
	return &libvirtStorageVolume{Mutex: pool.Mutex, name: def.Name, v: v}, nil
}

func (pool *libvirtStoragePool) ListVolumes() ([]virt.StorageVolume, error) {
	pool.Lock()
	defer pool.Unlock()
	volumes, err := pool.p.ListAllStorageVolumes(0)
	if err != nil {
		return nil, err
	}
	r := make([]virt.StorageVolume, len(volumes))
	for n, v := range volumes {
		name, err := v.GetName()
		if err != nil {
			return nil, err
		}
		// need to make a copy here
		curVolume := v
		r[n] = &libvirtStorageVolume{Mutex: pool.Mutex, name: name, v: &curVolume}
	}
	return r, nil
}

func (pool *libvirtStoragePool) LookupVolumeByName(name string) (virt.StorageVolume, error) {
	pool.Lock()
	defer pool.Unlock()
	v, err := pool.p.LookupStorageVolByName(name)
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_STORAGE_VOL {
			return nil, virt.ErrStorageVolumeNotFound
		}
		return nil, err
	}
	return &libvirtStorageVolume{Mutex: pool.Mutex, name: name, v: v}, nil
}

func (pool *libvirtStoragePool) RemoveVolumeByName(name string) error {
	vol, err := pool.LookupVolumeByName(name)
	switch {
	case err == virt.ErrStorageVolumeNotFound:
		return nil
	case err != nil:
		return err
	default:
		return vol.Remove()
	}
}

func (pool *libvirtStoragePool) XML() (*libvirtxml.StoragePool, error) {
	desc, err := pool.p.GetXMLDesc(libvirt.STORAGE_XML_INACTIVE)
	if err != nil {
		return nil, err
	}
	var p libvirtxml.StoragePool
	if err := p.Unmarshal(desc); err != nil {
		return nil, fmt.Errorf("error unmarshalling storage pool definition: %v", err)
	}
	return &p, nil
}

type libvirtStorageVolume struct {
	*sync.Mutex
	name string
	v    *libvirt.StorageVol
}

var _ virt.StorageVolume = &libvirtStorageVolume{}

func (volume *libvirtStorageVolume) Name() string {
	return volume.name
}

func (volume *libvirtStorageVolume) Size() (uint64, error) {
	volume.Lock()
	defer volume.Unlock()
	info, err := volume.v.GetInfo()
	if err != nil {
		return 0, err
	}
	return info.Capacity, nil
}

func (volume *libvirtStorageVolume) Path() (string, error) {
	volume.Lock()
	defer volume.Unlock()
	return volume.v.GetPath()
}

func (volume *libvirtStorageVolume) Remove() error {
	volume.Lock()
	defer volume.Unlock()
	return volume.v.Delete(0)
}

func (volume *libvirtStorageVolume) Format() error {
	volume.Lock()
	defer volume.Unlock()
	volPath, err := volume.v.GetPath()
	if err != nil {
		return fmt.Errorf("can't get volume path: %v", err)
	}
	return diskimage.FormatDisk(volPath)
}

func (volume *libvirtStorageVolume) XML() (*libvirtxml.StorageVolume, error) {
	desc, err := volume.v.GetXMLDesc(0)
	if err != nil {
		return nil, err
	}
	var v libvirtxml.StorageVolume
	if err := v.Unmarshal(desc); err != nil {
		return nil, fmt.Errorf("error unmarshalling storage volume definition: %v", err)
	}
	return &v, nil
}
