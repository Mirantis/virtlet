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

// TODO: move VolumeInfo here from storage_pool.go

// ErrStoragePoolNotFound error is returned by VirtStorageConnection's
// LookupByName() method when the pool in question cannot be found
var ErrStoragePoolNotFound = errors.New("storage pool not found")

// ErrStorageVolumeNotFound error is returned by VirtStoragePool's
// LookupVolumeByName() method when the volume in question cannot
// be found
var ErrStorageVolumeNotFound = errors.New("storage volume not found")

// VirtStorageConnection provides operations on the storage pools and storage volumes
type VirtStorageConnection interface {
	// CreateStoragePool creates a storage pool based on the specified definition
	CreateStoragePool(def *libvirtxml.StoragePool) (VirtStoragePool, error)
	// LookupByName tries to locate the storage pool by its
	// UUID. In case if the domain cannot be found but no other
	// error occurred, it returns ErrStoragePoolNotFound
	LookupStoragePoolByName(name string) (VirtStoragePool, error)
}

// VirtStoragePool represents a pool of volumes
type VirtStoragePool interface {
	// CreateStorageVol creates a new storage volume based on the specified definition
	CreateStorageVol(def *libvirtxml.StorageVolume) (VirtStorageVolume, error)
	// ListAllVolumes lists all storage volumes available in the pool
	ListAllVolumes() ([]VirtStorageVolume, error)
	// LookupVolumeByName tries to locate the storage volume by its
	// UUID. In case if the domain cannot be found but no other
	// error occurred, it returns ErrStorageVolumeNotFound
	LookupVolumeByName(name string) (VirtStorageVolume, error)
	// RemoveVolumeByName removes the storage volume with the
	// specified name
	RemoveVolumeByName(name string) error
}

type VirtStorageVolume interface {
	// Name returns the name of this storage volume
	Name() string
	// Size returns the size of this storage volume
	Size() (uint64, error)
	// Size returns the path to the file representing this storage volume
	Path() (string, error)
	// Remove removes this storage volume
	Remove() error
	// Format formats the volume as ext4 filesystem
	Format() error
}
