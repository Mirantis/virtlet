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
	"flag"
	"math/rand"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/manager"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
)

var (
	libvirtUri = flag.String("libvirt-uri", "qemu:///system",
		"Libvirt connection URI")
	pool = flag.String("pool", "default",
		"Storage pool in which the images should be stored")
	storageBackend = flag.String("storage-backend", "dir",
		"Libvirt storage pool type/backend")
	boltPath = flag.String("bolt-path", "/var/lib/virtlet/virtlet.db",
		"Path to the bolt database file")
	listen = flag.String("listen", "/run/virtlet.sock",
		"The unix socket to listen on, e.g. /run/virtlet.sock")
	cniPluginsDir = flag.String("cni-bin-dir", "/opt/cni/bin",
		"Path to CNI plugin binaries")
	cniConfigsDir = flag.String("cni-conf-dir", "/etc/cni/net.d",
		"Location of CNI configurations (first file name in lexicographic order will be chosen)")
	imageDownloadProtocol = flag.String("image-download-protocol", "https",
		"Image download protocol. Can be https (default) or http.")
	rawDevices = flag.String("raw-devices", "loop*",
		"Comma separated list of raw device glob patterns to which VM can have an access (with skipped /dev/ prefix)")
	fdServerSocketPath = flag.String("fd-server-socket-path", "/var/lib/virtlet/tapfdserver.sock",
		"Path to fd server socket")
)

const (
	WantTapManagerEnv         = "WANT_TAP_MANAGER"
	TapManagerConnectInterval = 200 * time.Millisecond
	TapManagerAttemptCount    = 50
)

func runVirtlet() {
	c := tapmanager.NewFDClient(*fdServerSocketPath)
	var err error
	for i := 0; i < TapManagerAttemptCount; i++ {
		time.Sleep(TapManagerConnectInterval)

		if err = c.Connect(); err == nil {
			break
		}
	}
	if err != nil {
		glog.Errorf("Failed to connect to tapmanager: %v", err)
		os.Exit(1)
	}
	server, err := manager.NewVirtletManager(*libvirtUri, *pool, *imageDownloadProtocol, *storageBackend, *boltPath, *rawDevices, c)
	if err != nil {
		glog.Errorf("Initializing server failed: %v", err)
		os.Exit(1)
	}
	glog.V(1).Infof("Starting server on socket %s", *listen)
	if err = server.Serve(*listen); err != nil {
		glog.Errorf("Serving failed: %v", err)
		os.Exit(1)
	}
}

func runTapManager() {
	src, err := tapmanager.NewTapFDSource(*cniPluginsDir, *cniConfigsDir)
	if err != nil {
		glog.Errorf("Error creating tap fd source: %v", err)
		os.Exit(1)
	}
	os.Remove(*fdServerSocketPath) // FIXME
	s := tapmanager.NewFDServer(*fdServerSocketPath, src)
	if err = s.Serve(); err != nil {
		glog.Errorf("FD server returned error: %v", err)
		os.Exit(1)
	}
	if err := libvirttools.ChownForEmulator(*fdServerSocketPath); err != nil {
		glog.Warningf("couldn't set tapmanager socket permissions: %v", err)
	}
	for {
		time.Sleep(1000 * time.Hour)
	}
}

func startTapManagerProcess() {
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), WantTapManagerEnv+"=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Here we make this process die with the main Virtlet process.
	// Note that this is Linux-specific, and also it may fail if virtlet is PID 1:
	// https://github.com/golang/go/issues/9263
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	if err := cmd.Start(); err != nil {
		glog.Errorf("error starting tapmanager process: %v", err)
		os.Exit(1)
	}
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	if os.Getenv(WantTapManagerEnv) == "" {
		startTapManagerProcess()
		runVirtlet()
	} else {
		runTapManager()
	}
}
