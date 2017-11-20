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

package integration

import (
	"testing"

	"google.golang.org/grpc"

	"os"

	"github.com/Mirantis/virtlet/pkg/manager"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	virtletSocket = "/tmp/virtlet.sock"
)

type fakeFDManager struct{}

var fdManager tapmanager.FDManager

func (m *fakeFDManager) AddFDs(key string, data interface{}) ([]byte, error) {
	return nil, nil
}

func (m *fakeFDManager) ReleaseFDs(key string) error {
	return nil
}

type VirtletManager struct {
	t       *testing.T
	manager *manager.VirtletManager
	conn    *grpc.ClientConn
	doneCh  chan struct{}
}

func NewVirtletManager(t *testing.T) *VirtletManager {
	return &VirtletManager{t: t}
}

func (v *VirtletManager) Run() {
	if v.manager != nil {
		v.t.Fatalf("virtlet manager already started")
	}

	dbFilename, err := utils.Tempfile()
	if err != nil {
		v.t.Fatalf("Can't create temp file: %v", err)
	}

	metadataStore, err := metadata.NewMetadataStore(dbFilename)
	if err != nil {
		v.t.Fatalf("Failed to create metadata store: %v", err)
	}

	os.Setenv("KUBERNETES_CLUSTER_URL", "")
	os.Setenv("VIRTLET_DISABLE_LOGGING", "true")
	v.manager, err = manager.NewVirtletManager(libvirtUri, "default", "http", "dir", "loop*", "", metadataStore, &fakeFDManager{})
	if err != nil {
		v.t.Fatalf("Failed to create VirtletManager: %v", err)
	}
	v.doneCh = make(chan struct{})
	go func() {
		if err := v.manager.Serve(virtletSocket); err != nil {
			v.t.Logf("VirtletManager result (expect closed network connection error): %v", err)
		}
		v.manager = nil
		close(v.doneCh)
	}()

	if err := waitForSocket(virtletSocket); err != nil {
		v.t.Fatalf("Couldn't connect to virtlet socket: %v", err)
	}

	v.conn, err = grpc.Dial(virtletSocket, grpc.WithInsecure(), grpc.WithDialer(utils.Dial))
	if err != nil {
		v.t.Fatalf("Couldn't connect to virtlet socket: %v", err)
	}
}

func (v *VirtletManager) Close() {
	if v.manager == nil {
		v.t.Fatalf("virtlet manager not started")
	}
	v.manager.Stop()
	<-v.doneCh
}
