/*
Copyright 2016-2018 Mirantis

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

package manager

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/api/virtlet.k8s/v1"
	"github.com/Mirantis/virtlet/pkg/image"
	"github.com/Mirantis/virtlet/pkg/imagetranslation"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/stream"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
)

const (
	tapManagerConnectInterval = 200 * time.Millisecond
	tapManagerAttemptCount    = 50
	streamerSocketPath        = "/var/lib/libvirt/streamer.sock"
	volumePoolName            = "volumes"
)

// VirtletManager wraps the Virtlet's Runtime and Image CRI services,
// as well as a gRPC server that provides access to them.
type VirtletManager struct {
	config         *v1.VirtletConfig
	metadataStore  metadata.Store
	fdManager      tapmanager.FDManager
	virtTool       *libvirttools.VirtualizationTool
	imageStore     image.Store
	runtimeService *VirtletRuntimeService
	imageService   *VirtletImageService
	server         *Server
}

// NewVirtletManager creates a new VirtletManager.
func NewVirtletManager(config *v1.VirtletConfig, fdManager tapmanager.FDManager) *VirtletManager {
	return &VirtletManager{config: config, fdManager: fdManager}
}

// Run sets up the environment for the runtime and image services and
// starts the gRPC listener. It doesn't return until the server is
// stopped or an error occurs.
func (v *VirtletManager) Run() error {
	var err error
	if v.fdManager == nil {
		client := tapmanager.NewFDClient(*v.config.FDServerSocketPath)
		for i := 0; i < tapManagerAttemptCount; i++ {
			time.Sleep(tapManagerConnectInterval)
			if err = client.Connect(); err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("failed to connect to tapmanager: %v", err)
		}
		v.fdManager = client
	}

	v.metadataStore, err = metadata.NewStore(*v.config.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to create metadata store: %v", err)
	}

	downloader := image.NewDownloader(*v.config.DownloadProtocol)
	v.imageStore = image.NewFileStore(*v.config.ImageDir, downloader, nil)
	v.imageStore.SetRefGetter(v.metadataStore.ImagesInUse)

	var translator image.Translator
	if !*v.config.SkipImageTranslation {
		if err = v1.RegisterCustomResourceTypes(); err != nil {
			return fmt.Errorf("failed to register image translation CRD: %v", err)
		}
		translator = imagetranslation.GetDefaultImageTranslator(*v.config.ImageTranslationConfigsDir, *v.config.EnableRegexpImageTranslation)
	} else {
		translator = imagetranslation.GetEmptyImageTranslator()
	}

	conn, err := libvirttools.NewConnection(*v.config.LibvirtURI)
	if err != nil {
		return fmt.Errorf("error establishing libvirt connection: %v", err)
	}

	virtConfig := libvirttools.VirtualizationConfig{
		DisableKVM:     *v.config.DisableKVM,
		EnableSriov:    *v.config.EnableSriov,
		VolumePoolName: volumePoolName,
	}
	if *v.config.RawDevices != "" {
		virtConfig.RawDevices = strings.Split(*v.config.RawDevices, ",")
	}

	var streamServer StreamServer
	if !*v.config.DisableLogging {
		s, err := stream.NewServer(streamerSocketPath, v.metadataStore)
		if err != nil {
			return fmt.Errorf("couldn't create stream server: %v", err)
		}

		err = s.Start()
		if err != nil {
			glog.Warning("Could not start stream server: %s", err)

		}
		streamServer = s
		virtConfig.StreamerSocketPath = streamerSocketPath
	}

	volSrc := libvirttools.GetDefaultVolumeSource()
	v.virtTool = libvirttools.NewVirtualizationTool(conn, conn, v.imageStore, v.metadataStore, volSrc, virtConfig)

	runtimeService := NewVirtletRuntimeService(v.virtTool, v.metadataStore, v.fdManager, streamServer, v.imageStore, nil)
	imageService := NewVirtletImageService(v.imageStore, translator)

	v.server = NewServer()
	v.server.Register(runtimeService, imageService)

	if err := v.recoverAndGC(); err != nil {
		// we consider recover / gc errors non-fatal
		glog.Warning(err)
	}

	glog.V(1).Infof("Starting server on socket %s", v.config.CRISocketPath)
	if err = v.server.Serve(*v.config.CRISocketPath); err != nil {
		return fmt.Errorf("serving failed: %v", err)
	}

	return nil
}

// Stop stops the gRPC listener of the VirtletManager, if it's active.
func (v *VirtletManager) Stop() {
	if v.server != nil {
		v.server.Stop()
	}
}

// recoverAndGC performs the initial actions during VirtletManager
// startup, including recovering network namespaces and performing
// garbage collection for both libvirt and the image store.
func (v *VirtletManager) recoverAndGC() error {
	var errors []string
	for _, err := range v.recoverNetworkNamespaces() {
		errors = append(errors, fmt.Sprintf("* error recovering VM network namespaces: %v", err))
	}

	for _, err := range v.virtTool.GarbageCollect() {
		errors = append(errors, fmt.Sprintf("* error performing libvirt GC: %v", err))
	}

	if err := v.imageStore.GC(); err != nil {
		errors = append(errors, fmt.Sprintf("* error during image GC: %v", err))
	}

	if len(errors) == 0 {
		return nil
	}

	return fmt.Errorf("errors encountered during recover / GC:\n%s", strings.Join(errors, "\n"))
}

// recoverNetworkNamespaces recovers all the active VM network namespaces
// from previous Virtlet run by scanning the metadata store and starting
// dhcp server for each namespace that's still active
func (v *VirtletManager) recoverNetworkNamespaces() (allErrors []error) {
	sandboxes, err := v.metadataStore.ListPodSandboxes(nil)
	if err != nil {
		allErrors = append(allErrors, err)
		return
	}

	for _, s := range sandboxes {
		psi, err := s.Retrieve()
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("can't retrieve PodSandboxInfo for sandbox id %q: %v", s.GetID(), err))
			continue
		}
		if psi == nil {
			allErrors = append(allErrors, fmt.Errorf("inconsistent database. Found pod %q sandbox but can not retrive its metadata", s.GetID()))
			continue
		}

		if err := v.fdManager.Recover(
			s.GetID(),
			tapmanager.GetFDPayload{
				ContainerSideNetwork: psi.ContainerSideNetwork,
				Description: &tapmanager.PodNetworkDesc{
					PodID:   s.GetID(),
					PodNs:   psi.Config.Namespace,
					PodName: psi.Config.Name,
				},
			},
		); err != nil {
			allErrors = append(allErrors, fmt.Errorf("error recovering netns for %q pod: %v", s.GetID(), err))
		}
	}
	return
}
