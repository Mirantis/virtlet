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

package main

import (
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Mirantis/virtlet/pkg/criproxy"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	cfg "k8s.io/kubernetes/pkg/apis/componentconfig/v1alpha1"
	"k8s.io/kubernetes/pkg/kubelet/dockershim"
	dockerremote "k8s.io/kubernetes/pkg/kubelet/dockershim/remote"
	"k8s.io/kubernetes/pkg/kubelet/dockertools"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"

	"github.com/golang/glog"
)

const (
	// XXX: fix this
	connectionTimeout = 30 * time.Second
	dockerShimSocket  = "/var/run/proxy-dockershim.sock"
)

var (
	listen = flag.String("listen", "/run/criproxy.sock",
		"The unix socket to listen on, e.g. /run/virtlet.sock")
	connect = flag.String("connect", "/var/run/dockershim.sock",
		"CRI runtime ids and unix socket(s) to connect to, e.g. /var/run/dockershim.sock,alt:/var/run/another.sock")
)

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

func startDockerShim() (string, error) {
	streamingAddr, err := getAddressForStreaming()
	if err != nil {
		return "", err
	}
	var kubeCfg cfg.KubeletConfiguration
	cfg.SetDefaults_KubeletConfiguration(&kubeCfg)
	pluginSettings := dockershim.NetworkPluginSettings{
		HairpinMode:       componentconfig.HairpinNone, // kubeCfg.HairpinMode, --- XXX
		NonMasqueradeCIDR: kubeCfg.NonMasqueradeCIDR,   // XXX: was being taken from kubelet object
		PluginName:        "cni",
		PluginConfDir:     "/etc/kubernetes/cni/net.d",
		PluginBinDir:      "/usr/lib/kubernetes/cni/bin", // XXX: cniBinDir
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

func main() {
	flag.Parse()

	addrs := strings.Split(*connect, ",")
	dockerStarted := false
	for n, addr := range addrs {
		if addr != "docker" {
			continue
		}
		if dockerStarted {
			glog.Errorf("More than one 'docker' endpoint is specified")
			os.Exit(1)
		}
		dockerEndpoint, err := startDockerShim()
		if err != nil {
			glog.Errorf("Failed to start docker-shim: %v", err)
			os.Exit(1)
		}
		addrs[n] = dockerEndpoint
		dockerStarted = true
	}

	proxy, err := criproxy.NewRuntimeProxy(addrs, connectionTimeout)
	if err != nil {
		glog.Errorf("Error starting CRI proxy: %v", err)
		os.Exit(1)
	}
	glog.V(1).Infof("Starting CRI proxy on socket %s", *listen)
	if err := proxy.Serve(*listen, nil); err != nil {
		glog.Errorf("Serving failed: %v", err)
		os.Exit(1)
	}
}
