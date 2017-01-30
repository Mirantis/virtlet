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
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Mirantis/virtlet/pkg/criproxy"
	"github.com/golang/glog"
)

const (
	// XXX: don't hardcode
	connectionTimeout = 30 * time.Second
)

var (
	listen = flag.String("listen", "/run/criproxy.sock",
		"The unix socket to listen on, e.g. /run/virtlet.sock")
	connect = flag.String("connect", "/var/run/dockershim.sock",
		"CRI runtime ids and unix socket(s) to connect to, e.g. /var/run/dockershim.sock,alt:/var/run/another.sock")
	kubeletConfigPath = flag.String("kletcfg", "/etc/criproxy/kubelet.conf", "path to saved kubelet config file")
	apiServerHost     = flag.String("apiserver", "", "apiserver URL")
)

func runCriProxy(connect, listen, savedConfigPath string) error {
	addrs := strings.Split(connect, ",")
	dockerStarted := false

	kubeCfg, err := criproxy.LoadKubeletConfig(savedConfigPath)
	if err != nil {
		return err
	}

	for n, addr := range addrs {
		if addr != "docker" {
			continue
		}
		if dockerStarted {
			return fmt.Errorf("More than one 'docker' endpoint is specified")
		}

		dockerEndpoint, err := criproxy.StartDockerShim(kubeCfg)
		if err != nil {
			return fmt.Errorf("Failed to start docker-shim: %v", err)
		}
		addrs[n] = dockerEndpoint
		dockerStarted = true
	}

	cleanupDone := false
	proxy, err := criproxy.NewRuntimeProxy(addrs, connectionTimeout, func() {
		if cleanupDone {
			return
		}
		// Perform the cleanup only when we're completely sure that kubelet
		// with proper runtime is active. Otherwise the old containers may
		// get recreated.
		if err := criproxy.RemoveContainersFromDefaultDockerRuntime(kubeCfg); err != nil {
			glog.Errorf("Container cleanup error: %v", err)
		}
		cleanupDone = true
	})
	if err != nil {
		return fmt.Errorf("Error starting CRI proxy: %v", err)
	}
	glog.V(1).Infof("Starting CRI proxy on socket %s", listen)
	if err := proxy.Serve(listen, nil); err != nil {
		return fmt.Errorf("Serving failed: %v", err)
	}
	return nil
}

func installCriProxy(execPath, savedConfigPath string) error {
	changed, err := criproxy.EnsureCRIProxy(&criproxy.BootstrapConfig{
		ApiServerHost:   *apiServerHost,
		ConfigzBaseUrl:  "https://127.0.0.1:10250",
		StatsBaseUrl:    "http://127.0.0.1:10255",
		SavedConfigPath: savedConfigPath,
		ProxyPath:       execPath,
		ProxyArgs: []string{
			// TODO: don't hardcode
			"-v",
			"3",
			"-alsologtostderr",
			"-connect",
			"docker,virtlet:/run/virtlet.sock",
		},
		ProxySocketPath: "/run/criproxy.sock",
	})
	if !changed {
		glog.V(1).Infof("Node configuration unchanged")
	}
	return err
}

func main() {
	install := flag.Bool("install", false, "install criproxy container")
	flag.Parse()

	var err error
	if *install {
		err = installCriProxy(os.Args[0], *kubeletConfigPath)
	} else {
		err = runCriProxy(*connect, *listen, *kubeletConfigPath)
	}
	if err != nil {
		glog.Error(err)
		os.Exit(1)
	}
}

// TODO: fix waiting for proxy socket
// Testing:
// /criproxy -alsologtostderr -v 2 -install -apiserver=http://172.30.0.2:8080
