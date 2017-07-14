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
	"github.com/Mirantis/virtlet/pkg/virt"
)

type FakeImageManager struct {
	rec  Recorder
	pool *FakeStoragePool
}

func NewFakeImageManager(rec Recorder, storagePool *FakeStoragePool) *FakeImageManager {
	return &FakeImageManager{
		rec:  rec,
		pool: storagePool,
	}
}

func (im *FakeImageManager) GetImageVolume(imageName string) (virt.VirtStorageVolume, error) {
	im.rec.Rec("GetImageVolume", imageName)
	return &FakeStorageVolume{
		rec:  im.rec,
		pool: im.pool,
		name: "fake volume name",
		path: "/fake/volume/path",
	}, nil
}
