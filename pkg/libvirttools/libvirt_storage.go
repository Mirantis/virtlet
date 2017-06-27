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
	"io"
	"os"

	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/diskimage"
	"github.com/Mirantis/virtlet/pkg/virt"
)

type LibvirtStorageConnection struct {
	conn *libvirt.Connect
}

var _ virt.VirtStorageConnection = &LibvirtStorageConnection{}

func newLibvirtStorageConnection(conn *libvirt.Connect) *LibvirtStorageConnection {
	return &LibvirtStorageConnection{conn: conn}
}

func (sc *LibvirtStorageConnection) CreateStoragePool(def *libvirtxml.StoragePool) (virt.VirtStoragePool, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Creating storage pool:\n%s", xml)
	p, err := sc.conn.StoragePoolCreateXML(xml, 0)
	if err != nil {
		return nil, err
	}
	return &LibvirtStoragePool{conn: sc.conn, p: p}, nil
}

func (sc *LibvirtStorageConnection) LookupStoragePoolByName(name string) (virt.VirtStoragePool, error) {
	p, err := sc.conn.LookupStoragePoolByName(name)
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_STORAGE_POOL {
			return nil, virt.ErrStoragePoolNotFound
		}
		return nil, err
	}
	return &LibvirtStoragePool{conn: sc.conn, p: p}, nil
}

type LibvirtStoragePool struct {
	conn *libvirt.Connect
	p    *libvirt.StoragePool
}

var _ virt.VirtStoragePool = &LibvirtStoragePool{}

func (pool *LibvirtStoragePool) CreateStorageVol(def *libvirtxml.StorageVolume) (virt.VirtStorageVolume, error) {
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
	return &LibvirtStorageVolume{name: def.Name, v: v}, nil
}

func (pool *LibvirtStoragePool) CreateStorageVolClone(def *libvirtxml.StorageVolume, from virt.VirtStorageVolume) (virt.VirtStorageVolume, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Creating storage volume clone:\n%s", xml)
	v, err := pool.p.StorageVolCreateXMLFrom(xml, from.(*LibvirtStorageVolume).v, 0)
	if err != nil {
		return nil, err
	}
	return &LibvirtStorageVolume{name: def.Name, v: v}, nil
}

func (pool *LibvirtStoragePool) ListAllVolumes() ([]virt.VirtStorageVolume, error) {
	volumes, err := pool.p.ListAllStorageVolumes(0)
	if err != nil {
		return nil, err
	}
	r := make([]virt.VirtStorageVolume, len(volumes))
	for n, v := range volumes {
		name, err := v.GetName()
		if err != nil {
			return nil, err
		}
		// need to make a copy here
		curVolume := v
		r[n] = &LibvirtStorageVolume{name: name, v: &curVolume}
	}
	return r, nil
}

func (pool *LibvirtStoragePool) LookupVolumeByName(name string) (virt.VirtStorageVolume, error) {
	v, err := pool.p.LookupStorageVolByName(name)
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_STORAGE_VOL {
			return nil, virt.ErrStorageVolumeNotFound
		}
		return nil, err
	}
	return &LibvirtStorageVolume{name: name, v: v}, nil
}

func (pool *LibvirtStoragePool) ImageToVolume(def *libvirtxml.StorageVolume, sourcePath string) (virt.VirtStorageVolume, error) {
	// if we have such image already in store - remove it
	existingVol, _ := pool.LookupVolumeByName(def.Name)
	if existingVol != nil {
		if err := existingVol.Remove(); err != nil {
			return nil, err
		}
	}

	f, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vol, err := pool.CreateStorageVol(def)
	if err != nil {
		return nil, err
	}

	stream, err := pool.conn.NewStream(0)
	if err != nil {
		return nil, err
	}

	err = vol.(*LibvirtStorageVolume).v.Upload(stream, 0, 0, 0)
	if err != nil {
		return nil, err
	}

	err = stream.SendAll(func(s *libvirt.Stream, n int) ([]byte, error) {
		buffer := make([]byte, n)

		nread, err := f.Read(buffer)
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			return nil, nil
		}

		return buffer[:nread], nil
	})

	if err == nil {
		err = stream.Finish()
	}

	if err != nil {
		return nil, err
	}

	return vol, nil
}

func (pool *LibvirtStoragePool) RemoveVolumeByName(name string) error {
	vol, err := pool.LookupVolumeByName(name)
	if err != nil {
		return err
	}
	return vol.Remove()
}

type LibvirtStorageVolume struct {
	name string
	v    *libvirt.StorageVol
}

var _ virt.VirtStorageVolume = &LibvirtStorageVolume{}

func (volume *LibvirtStorageVolume) Name() string {
	return volume.name
}

func (volume *LibvirtStorageVolume) Size() (uint64, error) {
	info, err := volume.v.GetInfo()
	if err != nil {
		return 0, err
	}
	return info.Capacity, nil
}

func (volume *LibvirtStorageVolume) Path() (string, error) {
	return volume.v.GetPath()
}

func (volume *LibvirtStorageVolume) Remove() error {
	return volume.v.Delete(0)
}

func (volume *LibvirtStorageVolume) Format() error {
	volPath, err := volume.Path()
	if err != nil {
		return fmt.Errorf("can't get volume path: %v", err)
	}
	return diskimage.FormatDisk(volPath)
}
