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

Based on kubelet code from Kubernetes project.
Original copyright notice follows:

Copyright 2015 The Kubernetes Authors.

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

package dockershim

import (
	"errors"
	goflag "flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/spf13/pflag"

	"k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/kubernetes/cmd/kubelet/app/options"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	"k8s.io/kubernetes/pkg/kubelet"
	dshim "k8s.io/kubernetes/pkg/kubelet/dockershim"
	"k8s.io/kubernetes/pkg/kubelet/dockershim/libdocker"
	dockerremote "k8s.io/kubernetes/pkg/kubelet/dockershim/remote"
	"k8s.io/kubernetes/pkg/kubelet/server/streaming"
	nodeutil "k8s.io/kubernetes/pkg/util/node"
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

// doRunDockershim only starts the dockershim in current process. This is only used for cri validate testing purpose
// TODO(random-liu): Move this to a separate binary.
func doRunDockershim(c *componentconfig.KubeletConfiguration, r *options.ContainerRuntimeOptions) error {
	streamingAddr, err := getAddressForStreaming()
	if err != nil {
		return err
	}

	// Create docker client.
	dockerClient := libdocker.ConnectToDockerOrDie(r.DockerEndpoint, c.RuntimeRequestTimeout.Duration,
		r.ImagePullProgressDeadline.Duration)

	// Initialize network plugin settings.
	binDir := r.CNIBinDir
	if binDir == "" {
		binDir = r.NetworkPluginDir
	}
	nh := &kubelet.NoOpLegacyHost{}
	pluginSettings := dshim.NetworkPluginSettings{
		HairpinMode:       componentconfig.HairpinMode(c.HairpinMode),
		NonMasqueradeCIDR: c.NonMasqueradeCIDR,
		PluginName:        r.NetworkPluginName,
		PluginConfDir:     r.CNIConfDir,
		PluginBinDir:      binDir,
		MTU:               int(r.NetworkPluginMTU),
		LegacyRuntimeHost: nh,
	}
	glog.V(3).Infof("Docker plugin settings: %s", spew.Sdump(pluginSettings))

	// Initialize streaming configuration. (Not using TLS now)
	streamingConfig := &streaming.Config{
		Addr:                            streamingAddr + ":12345", // FIXME
		StreamIdleTimeout:               c.StreamingConnectionIdleTimeout.Duration,
		StreamCreationTimeout:           streaming.DefaultConfig.StreamCreationTimeout,
		SupportedRemoteCommandProtocols: streaming.DefaultConfig.SupportedRemoteCommandProtocols,
		SupportedPortForwardProtocols:   streaming.DefaultConfig.SupportedPortForwardProtocols,
	}

	ds, err := dshim.NewDockerService(dockerClient, c.SeccompProfileRoot, r.PodSandboxImage,
		streamingConfig, &pluginSettings, c.RuntimeCgroups, c.CgroupDriver, r.DockerExecHandlerName, r.DockershimRootDirectory,
		r.DockerDisableSharedPID)
	if err != nil {
		return err
	}
	if err := ds.Start(); err != nil {
		return err
	}

	glog.V(2).Infof("Starting the GRPC server for the docker CRI shim.")
	server := dockerremote.NewDockerServer(c.RemoteRuntimeEndpoint, ds)
	if err := server.Start(); err != nil {
		return err
	}

	// Start the streaming server
	return http.ListenAndServe(streamingConfig.Addr, ds)
}

// initFlags normalizes, parses, then logs the command line flags
func initFlags(arguments []string) {
	pflag.CommandLine.SetNormalizeFunc(flag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	pflag.CommandLine.Parse(arguments)
	pflag.VisitAll(func(flag *pflag.Flag) {
		glog.V(4).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})
}

type KubeletWrapper struct {
	s *options.KubeletServer
}

func NewKubeletWrapper(arguments []string) *KubeletWrapper {
	s := options.NewKubeletServer()
	s.AddFlags(pflag.CommandLine)
	initFlags(arguments)
	return &KubeletWrapper{s}
}

func (k *KubeletWrapper) RunDockershim() {
	logs.InitLogs()
	defer logs.FlushLogs()
	glog.V(3).Infof("RunDockershim(): Kubelet config: %s", spew.Sdump(k.s.KubeletConfiguration))

	if err := doRunDockershim(&k.s.KubeletConfiguration, &k.s.ContainerRuntimeOptions); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func (k *KubeletWrapper) NodeName() string {
	// NOTE: this will not work with cloud providers, but that's
	// not the kind of situation we're expecting to happen when
	// using CRI proxy bootstrap where this func is to be used
	return nodeutil.GetHostname(k.s.HostnameOverride)
}

func (k *KubeletWrapper) DockerEndpoint() string {
	return k.s.DockerEndpoint
}

func (k *KubeletWrapper) Endpoint() string {
	ep := k.s.RemoteRuntimeEndpoint
	if strings.HasPrefix(ep, "unix://") {
		return ep[7:]
	}
	// FIXME: probably should error here
	return ep
}
