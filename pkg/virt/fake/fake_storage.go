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
	"path"
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
	rec   Recorder
	pools map[string]*FakeStoragePool
}

func NewFakeStorageConnection(rec Recorder) *FakeStorageConnection {
	return &FakeStorageConnection{
		rec:   rec,
		pools: make(map[string]*FakeStoragePool),
	}
}

func (sc *FakeStorageConnection) CreateStoragePool(def *libvirtxml.StoragePool) (virt.VirtStoragePool, error) {
	sc.rec.Rec("CreateStoragePool", def)
	if _, found := sc.pools[def.Name]; found {
		return nil, fmt.Errorf("storage pool already exists: %v", def.Name)
	}
	poolPath := "/"
	if def.Target != nil {
		poolPath = def.Target.Path
	}
	p := newFakeStoragePool(NewChildRecorder(sc.rec, def.Name), def.Name, poolPath)
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
	rec     Recorder
	name    string
	path    string
	volumes map[string]*FakeStorageVolume
}

func newFakeStoragePool(rec Recorder, name, poolPath string) *FakeStoragePool {
	return &FakeStoragePool{
		rec:     rec,
		name:    name,
		path:    poolPath,
		volumes: make(map[string]*FakeStorageVolume),
	}
}

func (p *FakeStoragePool) createStorageVol(def *libvirtxml.StorageVolume) (virt.VirtStorageVolume, error) {
	if _, found := p.volumes[def.Name]; found {
		return nil, fmt.Errorf("storage volume already exists: %v", def.Name)
	}
	v, err := newFakeStorageVolume(NewChildRecorder(p.rec, def.Name), p, def)
	if err != nil {
		return nil, err
	}
	p.volumes[def.Name] = v
	return v, nil
}

func (p *FakeStoragePool) CreateStorageVol(def *libvirtxml.StorageVolume) (virt.VirtStorageVolume, error) {
	p.rec.Rec("CreateStorageVol", def)
	return p.createStorageVol(def)
}

func (p *FakeStoragePool) CreateStorageVolClone(def *libvirtxml.StorageVolume, from virt.VirtStorageVolume) (virt.VirtStorageVolume, error) {
	p.rec.Rec("CreateStorageVolClone", map[string]interface{}{
		"def":  def,
		"from": from.(*FakeStorageVolume).descriptiveName(),
	})
	d := *def
	d.Capacity = &libvirtxml.StorageVolumeSize{Unit: "b", Value: from.(*FakeStorageVolume).size}
	return p.createStorageVol(&d)
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

func (p *FakeStoragePool) removeVolumeByName(name string) error {
	if _, found := p.volumes[name]; !found {
		return virt.ErrStorageVolumeNotFound
	}
	delete(p.volumes, name)
	return nil
}

func (p *FakeStoragePool) RemoveVolumeByName(name string) error {
	p.rec.Rec("RemoveVolumeByName", name)
	return p.removeVolumeByName(name)
}

type FakeStorageVolume struct {
	rec  Recorder
	pool *FakeStoragePool
	name string
	path string
	size uint64
}

func newFakeStorageVolume(rec Recorder, pool *FakeStoragePool, def *libvirtxml.StorageVolume) (*FakeStorageVolume, error) {
	volPath := ""
	if def.Target != nil {
		volPath = def.Target.Path
	}
	if volPath == "" {
		volPath = path.Join(pool.path, def.Name)
	}

	v := &FakeStorageVolume{
		rec:  rec,
		pool: pool,
		name: def.Name,
		path: volPath,
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

func (v *FakeStorageVolume) descriptiveName() string {
	return v.pool.name + "." + v.name
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
	v.rec.Rec("Remove", nil)
	return v.pool.removeVolumeByName(v.name)
}

func (v *FakeStorageVolume) Format() error {
	v.rec.Rec("Format", nil)
	return nil
}
