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

// ErrStoragePoolNotFound error is returned by StorageConnection's
// LookupByName() method when the pool in question cannot be found
var ErrStoragePoolNotFound = errors.New("storage pool not found")

// ErrStorageVolumeNotFound error is returned by StoragePool's
// LookupVolumeByName() method when the volume in question cannot
// be found
var ErrStorageVolumeNotFound = errors.New("storage volume not found")

// StorageConnection provides operations on the storage pools and storage volumes
type StorageConnection interface {
	// CreateStoragePool creates a storage pool based on the specified definition
	CreateStoragePool(def *libvirtxml.StoragePool) (StoragePool, error)
	// LookupByName tries to locate the storage pool by its
	// UUID. In case if the domain cannot be found but no other
	// error occurred, it returns ErrStoragePoolNotFound
	LookupStoragePoolByName(name string) (StoragePool, error)
	// ListPools() retrieves the list of pools
	ListPools() ([]StoragePool, error)
	// PutFiles add files to the specified image.
	PutFiles(imagePath string, files map[string][]byte) error
}

// StoragePool represents a pool of volumes
type StoragePool interface {
	// CreateStorageVol creates a new storage volume based on the specified definition
	CreateStorageVol(def *libvirtxml.StorageVolume) (StorageVolume, error)
	// ListVolumes lists all the storage volumes available in the pool
	ListVolumes() ([]StorageVolume, error)
	// LookupVolumeByName tries to locate the storage volume by its
	// UUID. In case if the domain cannot be found but no other
	// error occurred, it returns ErrStorageVolumeNotFound
	LookupVolumeByName(name string) (StorageVolume, error)
	// RemoveVolumeByName removes the storage volume with the
	// specified name
	RemoveVolumeByName(name string) error
	// XML retrieves xml definition of the pool
	XML() (*libvirtxml.StoragePool, error)
}

// StorageVolume represents a particular volume in pool
type StorageVolume interface {
	// Name returns the name of this storage volume
	Name() string
	// Size returns the size of this storage volume
	Size() (uint64, error)
	// Path returns the path to the file representing this storage volume
	Path() (string, error)
	// Remove removes this storage volume
	Remove() error
	// Format formats the volume as ext4 filesystem
	Format() error
	// XML retrieves xml definition of the volume
	XML() (*libvirtxml.StorageVolume, error)
}
