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

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mirantis/virtlet/pkg/criproxy"
	"github.com/Mirantis/virtlet/pkg/dockershim"
	"github.com/golang/glog"
)

const (
	// XXX: don't hardcode
	connectionTimeout            = 30 * time.Second
	dockershimExecutableName     = "dockershim"
	standardDockershimSocketPath = "/var/run/dockershim.sock"
)

var (
	listen = flag.String("listen", "/run/criproxy.sock",
		"The unix socket to listen on, e.g. /run/virtlet.sock")
	connect = flag.String("connect", "/var/run/dockershim.sock",
		"CRI runtime ids and unix socket(s) to connect to, e.g. /var/run/dockershim.sock,alt:/var/run/another.sock")
	nodeInfoPath  = flag.String("nodeinfo", "/etc/criproxy/node.conf", "path to saved node info (bootstrap-only)")
	apiServerHost = flag.String("apiserver", "", "apiserver URL")
)

// runCriProxy starts CRI proxy and optionally a dockershim
func runCriProxy(connect, listen, nodeInfoPath string) error {
	firstRun := false

	ni, err := criproxy.LoadNodeInfo(nodeInfoPath)
	if err != nil {
		return err
	}

	if ni.FirstRun {
		firstRun = true
		ni.FirstRun = false
		if err := ni.Write(nodeInfoPath); err != nil {
			return err
		}
	}

	addrs := strings.Split(connect, ",")
	dockerStarted := false

	for n, addr := range addrs {
		if addr != "docker" {
			continue
		}
		if dockerStarted {
			return fmt.Errorf("More than one 'docker' endpoint is specified")
		}

		kw := dockershim.NewKubeletWrapper(ni.KubeletArgs)
		addrs[n] = kw.Endpoint()
		glog.V(1).Infof("Starting dockershim on %q", kw.Endpoint())
		go kw.RunDockershim()
		dockerStarted = true
	}

	shouldClean := firstRun
	proxy, err := criproxy.NewRuntimeProxy(addrs, connectionTimeout, func() {
		// must do this after the old kubelet has exited
		if shouldClean {
			shouldClean = false
			if err := criproxy.RemoveKubeDNSContainers(ni.DockerEndpoint); err != nil {
				glog.Warningf("failed to clean up old containers: %v", err)
			}
		}
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

// grabKubeletInfo writes node info file based on the information
// obtained from running kubelet's command line. This function must be
// run from the host mount & uts namespaces.
func grabKubeletInfo(nodeInfoPath string) error {
	pid, err := criproxy.GetPidFromSocket(standardDockershimSocketPath)
	if err != nil {
		return err
	}

	ni, err := criproxy.NodeInfoFromCommandLine(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return err
	}
	kw := dockershim.NewKubeletWrapper(ni.KubeletArgs)
	ni.NodeName = kw.NodeName()
	ni.DockerEndpoint = kw.DockerEndpoint()
	ni.FirstRun = true

	glog.V(1).Infof("Writing node info file %q: %#v", nodeInfoPath, ni)
	return ni.Write(nodeInfoPath)
}

// installCriProxy installs CRI proxy on the host. It must be run
// from within a pod.
func installCriProxy(execPath, nodeInfoPath string) error {
	ni, err := criproxy.LoadNodeInfo(nodeInfoPath)
	if err != nil {
		return err
	}
	bootstrap := criproxy.NewBootstrap(&criproxy.BootstrapConfig{
		ApiServerHost:  *apiServerHost,
		ConfigzBaseUrl: "https://127.0.0.1:10250",
		ProxyPath:      execPath,
		ProxyArgs: []string{
			// TODO: don't hardcode
			"-v",
			"3",
			"-alsologtostderr",
			"-connect",
			"docker,virtlet:/run/virtlet.sock",
		},
		ProxySocketPath: "/run/criproxy.sock",
		NodeInfo:        ni,
	}, nil)
	changed, err := bootstrap.EnsureCRIProxy()
	if err == nil && !changed {
		glog.V(1).Infof("Node configuration unchanged")
	}
	return err
}

func main() {
	execPath, err := os.Executable()
	if err != nil {
		glog.Error("Can't get criproxy executable path: %v", err)
		os.Exit(1)
	}
	if filepath.Base(execPath) == dockershimExecutableName {
		kw := dockershim.NewKubeletWrapper(os.Args)
		kw.RunDockershim()
	} else {
		grab := flag.Bool("grab", false, "grab kubelet command line and node name")
		install := flag.Bool("install", false, "install criproxy container")
		flag.Parse()

		var err error
		switch {
		case *grab && *install:
			err = errors.New("can't specify both -grab and -install")
		case *grab:
			err = grabKubeletInfo(*nodeInfoPath)
		case *install:
			err = installCriProxy(execPath, *nodeInfoPath)
		default:
			err = runCriProxy(*connect, *listen, *nodeInfoPath)
		}
		if err != nil {
			glog.Error(err)
			os.Exit(1)
		}
	}
}

// Testing:
// /dind/criproxy -alsologtostderr -v 20 -grab
// /dind/criproxy -alsologtostderr -v 20 -install -apiserver=http://kube-master:8080
