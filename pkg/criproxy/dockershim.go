/*
Copyright 2016 Mirantis

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

package criproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/golang/glog"

	dockerclient "github.com/docker/engine-api/client"

	"k8s.io/kubernetes/pkg/apis/componentconfig"
	cfg "k8s.io/kubernetes/pkg/apis/componentconfig/v1alpha1"
	"k8s.io/kubernetes/pkg/kubelet/dockershim"
	dockerremote "k8s.io/kubernetes/pkg/kubelet/dockershim/remote"
	"k8s.io/kubernetes/pkg/kubelet/dockertools"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
)

const (
	dockerShimSocket = "/var/run/proxy-dockershim.sock"
	// This label is used to identify the containers from non-CRI docker runtime
	plainDockerRuntimeContainerLabel = "io.kubernetes.container.hash"
)

// effectiveHairpinMode determines the effective hairpin mode given the
// configured mode, container runtime, and whether cbr0 should be configured.
// From kubelet_network.go
func effectiveHairpinMode(hairpinMode componentconfig.HairpinMode, containerRuntime string, networkPlugin string) (componentconfig.HairpinMode, error) {
	// The hairpin mode setting doesn't matter if:
	// - We're not using a bridge network. This is hard to check because we might
	//   be using a plugin.
	// - It's set to hairpin-veth for a container runtime that doesn't know how
	//   to set the hairpin flag on the veth's of containers. Currently the
	//   docker runtime is the only one that understands this.
	// - It's set to "none".
	if hairpinMode == componentconfig.PromiscuousBridge || hairpinMode == componentconfig.HairpinVeth {
		// Only on docker.
		if containerRuntime != "docker" {
			glog.Warningf("Hairpin mode set to %q but container runtime is %q, ignoring", hairpinMode, containerRuntime)
			return componentconfig.HairpinNone, nil
		}
		if hairpinMode == componentconfig.PromiscuousBridge && networkPlugin != "kubenet" {
			// This is not a valid combination, since promiscuous-bridge only works on kubenet. Users might be using the
			// default values (from before the hairpin-mode flag existed) and we
			// should keep the old behavior.
			glog.Warningf("Hairpin mode set to %q but kubenet is not enabled, falling back to %q", hairpinMode, componentconfig.HairpinVeth)
			return componentconfig.HairpinVeth, nil
		}
	} else if hairpinMode != componentconfig.HairpinNone {
		return "", fmt.Errorf("unknown value: %q", hairpinMode)
	}
	return hairpinMode, nil
}

func getAddressForStreaming() (string, error) {
	// FIXME: see kubelet.setNodeAddress
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	addrs, err := net.LookupIP(hostname)
	for _, addr := range addrs {
		if !addr.IsLoopback() && addr.To4() != nil {
			return addr.String(), nil
		}
	}
	return "", errors.New("unable to get IP address")
}

func StartDockerShim(kubeCfg *cfg.KubeletConfiguration) (string, error) {
	hairpinMode, err := effectiveHairpinMode(componentconfig.HairpinMode(kubeCfg.HairpinMode), kubeCfg.ContainerRuntime, kubeCfg.NetworkPluginName)
	if err != nil {
		return "", fmt.Errorf("invalid hairpin mode: %v", err)
	}

	streamingAddr, err := getAddressForStreaming()
	if err != nil {
		return "", err
	}

	cniBinDir := kubeCfg.CNIBinDir
	if cniBinDir == "" {
		cniBinDir = kubeCfg.NetworkPluginDir
	}
	pluginSettings := dockershim.NetworkPluginSettings{
		HairpinMode:       hairpinMode,
		NonMasqueradeCIDR: kubeCfg.NonMasqueradeCIDR,
		PluginName:        kubeCfg.NetworkPluginName,
		PluginConfDir:     kubeCfg.CNIConfDir,
		PluginBinDir:      cniBinDir,
		MTU:               int(kubeCfg.NetworkPluginMTU),
	}
	dockerClient := dockertools.ConnectToDockerOrDie(kubeCfg.DockerEndpoint, kubeCfg.RuntimeRequestTimeout.Duration)

	streamingConfig := &streaming.Config{
		Addr:                  streamingAddr + ":12345", // FIXME
		StreamIdleTimeout:     kubeCfg.StreamingConnectionIdleTimeout.Duration,
		StreamCreationTimeout: streaming.DefaultConfig.StreamCreationTimeout,
		SupportedProtocols:    streaming.DefaultConfig.SupportedProtocols,
	}

	ds, err := dockershim.NewDockerService(dockerClient, kubeCfg.SeccompProfileRoot, kubeCfg.PodInfraContainerImage, streamingConfig, &pluginSettings, kubeCfg.RuntimeCgroups)
	if err != nil {
		return "", err
	}

	httpServer := &http.Server{
		Addr:    streamingConfig.Addr,
		Handler: ds,
		// TODO: TLSConfig?
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil {
			glog.Errorf("Failed to start http server: %v", err)
		}
	}()

	server := dockerremote.NewDockerServer(dockerShimSocket, ds)
	glog.V(2).Infof("Starting the GRPC server for the docker CRI shim.")
	if err := server.Start(); err != nil {
		return "", err
	}

	return dockerShimSocket, nil
}

func RemoveContainersFromDefaultDockerRuntime(kubeCfg *cfg.KubeletConfiguration) error {
	// The following is the same as this command:
	// docker ps -f label=io.kubernetes.container.hash -qa|xargs docker rm -fv
	client, err := dockerclient.NewClient(kubeCfg.DockerEndpoint, "", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}

	ctx := context.Background()
	if err := removeContainersByLabels(ctx, client, plainDockerRuntimeContainerLabel); err != nil {
		return fmt.Errorf("failed to remove the containers from plain docker runtime: %v", err)
	}
	return nil
}

// TODO: kill of 'plain' docker containers:
// docker ps -f label=io.kubernetes.container.hash -qa|xargs docker rm -fv
