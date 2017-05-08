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
	"io"
	"os"

	libvirt "github.com/libvirt/libvirt-go"
)

type StorageOperations interface {
	CreateFromXML(xmlConfig string) (*libvirt.StoragePool, error)
	CreateVolFromXML(pool *libvirt.StoragePool, xmlConfig string) (*libvirt.StorageVol, error)
	CreateVolCloneFromXML(pool *libvirt.StoragePool, xmlConfig string, from *libvirt.StorageVol) (*libvirt.StorageVol, error)
	ListAllVolumes(pool *libvirt.StoragePool) ([]libvirt.StorageVol, error)
	LookupByName(name string) (*libvirt.StoragePool, error)
	LookupVolumeByName(pool *libvirt.StoragePool, name string) (*libvirt.StorageVol, error)
	RemoveVolume(volume *libvirt.StorageVol) error
	VolumeGetInfo(volume *libvirt.StorageVol) (*libvirt.StorageVolInfo, error)
	VolumeGetName(volume *libvirt.StorageVol) (string, error)
	VolumeGetPath(volume *libvirt.StorageVol) (string, error)

	PullImageToVolume(pool *libvirt.StoragePool, shortName, filepath, volXML string) error
}

type LibvirtStorageOperations struct {
	conn *libvirt.Connect
}

func NewLibvirtStorageOperations(conn *libvirt.Connect) StorageOperations {
	return LibvirtStorageOperations{conn: conn}
}

func (l LibvirtStorageOperations) CreateFromXML(xmlConfig string) (*libvirt.StoragePool, error) {
	return l.conn.StoragePoolCreateXML(xmlConfig, 0)
}

func (l LibvirtStorageOperations) CreateVolFromXML(pool *libvirt.StoragePool, xmlConfig string) (*libvirt.StorageVol, error) {
	return pool.StorageVolCreateXML(xmlConfig, 0)
}

func (l LibvirtStorageOperations) CreateVolCloneFromXML(pool *libvirt.StoragePool, xmlConfig string, from *libvirt.StorageVol) (*libvirt.StorageVol, error) {
	return pool.StorageVolCreateXMLFrom(xmlConfig, from, 0)
}

func (l LibvirtStorageOperations) ListAllVolumes(pool *libvirt.StoragePool) ([]libvirt.StorageVol, error) {
	return pool.ListAllStorageVolumes(0)
}

func (l LibvirtStorageOperations) LookupByName(name string) (*libvirt.StoragePool, error) {
	return l.conn.LookupStoragePoolByName(name)
}

func (l LibvirtStorageOperations) LookupVolumeByName(pool *libvirt.StoragePool, name string) (*libvirt.StorageVol, error) {
	return pool.LookupStorageVolByName(name)
}

func (l LibvirtStorageOperations) RemoveVolume(volume *libvirt.StorageVol) error {
	return volume.Delete(0)
}

func (l LibvirtStorageOperations) VolumeGetInfo(volume *libvirt.StorageVol) (*libvirt.StorageVolInfo, error) {
	return volume.GetInfo()
}

func (l LibvirtStorageOperations) VolumeGetName(volume *libvirt.StorageVol) (string, error) {
	return volume.GetName()
}

func (l LibvirtStorageOperations) VolumeGetPath(volume *libvirt.StorageVol) (string, error) {
	return volume.GetPath()
}

func (l LibvirtStorageOperations) PullImageToVolume(pool *libvirt.StoragePool, shortName, filepath, volXML string) error {
	// if we have such image already in store - just ignore request
	existing_vol, _ := l.LookupVolumeByName(pool, shortName)
	if existing_vol != nil {
		return nil
	}

	f, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer f.Close()

	vol, err := l.CreateVolFromXML(pool, volXML)
	if err != nil {
		return err
	}

	stream, err := l.conn.NewStream(0)
	if err != nil {
		return err
	}

	err = vol.Upload(stream, 0, 0, 0)
	if err != nil {
		return err
	}

	err = stream.SendAll(func(s *libvirt.Stream, n int) ([]byte, error) {
		buffer := make([]byte, n)

		readed, err := f.Read(buffer)
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			return nil, nil
		}

		return buffer[:readed], nil
	})

	if err != nil {
		return err
	}

	return stream.Finish()
}
