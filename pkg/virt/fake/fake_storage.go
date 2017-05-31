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
	"os"
	"sort"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/virt"
)

var capacityUnits = map[string]uint64{
	"b":     1,
	"bytes": 1,
	"KB":    1000,
	"k":     1024,
	"KiB":   1024,
	"":      1024, // libvirt defaults to KiB
	"MB":    1000000,
	"M":     1048576,
	"MiB":   1048576,
	"GB":    1000000000,
	"G":     1073741824,
	"GiB":   1073741824,
	"TB":    1000000000000,
	"T":     1099511627776,
	"TiB":   1099511627776,
}

type FakeStorageConnection struct {
	pools map[string]*FakeStoragePool
}

func NewFakeStorageConnection() *FakeStorageConnection {
	return &FakeStorageConnection{make(map[string]*FakeStoragePool)}
}

func (sc *FakeStorageConnection) CreateStoragePool(def *libvirtxml.StoragePool) (virt.VirtStoragePool, error) {
	if _, found := sc.pools[def.Name]; found {
		return nil, fmt.Errorf("storage pool already exists: %v", def.Name)
	}
	p := newFakeStoragePool()
	sc.pools[def.Name] = p
	return p, nil
}

func (sc *FakeStorageConnection) LookupStoragePoolByName(name string) (virt.VirtStoragePool, error) {
	if p, found := sc.pools[name]; found {
		return p, nil
	} else {
		return nil, virt.ErrStoragePoolNotFound
	}
}

type FakeStoragePool struct {
	volumes map[string]*FakeStorageVolume
}

func newFakeStoragePool() *FakeStoragePool {
	return &FakeStoragePool{volumes: make(map[string]*FakeStorageVolume)}
}

func (p *FakeStoragePool) CreateStorageVol(def *libvirtxml.StorageVolume) (virt.VirtStorageVolume, error) {
	if _, found := p.volumes[def.Name]; found {
		return nil, fmt.Errorf("storage volume already exists: %v", def.Name)
	}
	v, err := newFakeStorageVolume(p, def)
	if err != nil {
		return nil, err
	}
	p.volumes[def.Name] = v
	return v, nil
}

func (p *FakeStoragePool) CreateStorageVolClone(def *libvirtxml.StorageVolume, from virt.VirtStorageVolume) (virt.VirtStorageVolume, error) {
	d := *def
	d.Capacity = &libvirtxml.StorageVolumeSize{Unit: "b", Value: from.(*FakeStorageVolume).size}
	return p.CreateStorageVol(&d)
}

func (p *FakeStoragePool) ListAllVolumes() ([]virt.VirtStorageVolume, error) {
	r := make([]virt.VirtStorageVolume, len(p.volumes))
	names := make([]string, 0, len(p.volumes))
	for name, _ := range p.volumes {
		names = append(names, name)
	}
	sort.Strings(names)
	for n, name := range names {
		r[n] = p.volumes[name]
	}
	return r, nil
}

func (p *FakeStoragePool) LookupVolumeByName(name string) (virt.VirtStorageVolume, error) {
	if v, found := p.volumes[name]; found {
		return v, nil
	}
	return nil, virt.ErrStorageVolumeNotFound
}

func (p *FakeStoragePool) ImageToVolume(def *libvirtxml.StorageVolume, sourcePath string) (virt.VirtStorageVolume, error) {
	fi, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("os.Stat(): %q: %v", sourcePath, err)
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("ImageToVolume(): %q is a directory", sourcePath)
	}
	return p.CreateStorageVol(def)
}

func (p *FakeStoragePool) RemoveVolumeByName(name string) error {
	if _, found := p.volumes[name]; !found {
		return virt.ErrStorageVolumeNotFound
	}
	delete(p.volumes, name)
	return nil
}

type FakeStorageVolume struct {
	pool *FakeStoragePool
	name string
	path string
	size uint64
}

func newFakeStorageVolume(pool *FakeStoragePool, def *libvirtxml.StorageVolume) (*FakeStorageVolume, error) {
	path := ""
	if def.Target != nil {
		path = def.Target.Path
	}

	v := &FakeStorageVolume{
		pool: pool,
		name: def.Name,
		path: path,
	}
	if def.Capacity != nil {
		coef, found := capacityUnits[def.Capacity.Unit]
		if !found {
			return nil, fmt.Errorf("bad capacity units: %q", def.Capacity.Unit)
		}
		v.size = def.Capacity.Value * coef
	}

	return v, nil
}

func (v *FakeStorageVolume) Name() string {
	return v.name
}

func (v *FakeStorageVolume) Size() (uint64, error) {
	return v.size, nil
}

func (v *FakeStorageVolume) Path() (string, error) {
	return v.path, nil
}

func (v *FakeStorageVolume) Remove() error {
	return v.pool.RemoveVolumeByName(v.name)
}
