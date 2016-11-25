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
	"os"

	"github.com/Mirantis/virtlet/pkg/manager"

	"github.com/golang/glog"
)

var (
	libvirtUri = flag.String("libvirt-uri", "qemu:///system",
		"Libvirt connection URI")
	pool = flag.String("pool", "default",
		"Storage pool in which the images should be stored")
	storageBackend = flag.String("storage-backend", "dir",
		"Libvirt storage pool type/backend")
	boltPath = flag.String("bolt-path", "/var/data/virtlet/virtlet.db",
		"Path to the bolt database file")
	listen = flag.String("listen", "/run/virtlet.sock",
		"The unix socket to listen on, e.g. /run/virtlet.sock")
	cniPluginsDir = flag.String("cni-bin-dir", "/opt/cni/bin",
		"Path to CNI plugin binaries")
	cniConfigsDir = flag.String("cni-conf-dir", "/etc/cni/net.d",
		"Location of CNI configurations (first file name in lexicographic order will be chosen)")
)

func main() {
	flag.Parse()

	server, err := manager.NewVirtletManager(*libvirtUri, *pool, *storageBackend, *boltPath, *cniPluginsDir, *cniConfigsDir)
	if err != nil {
		glog.Errorf("Initializing server failed: %v", err)
		os.Exit(1)
	}
	glog.V(1).Infof("Starting server on socket %s", *listen)
	if err = server.Serve(*listen); err != nil {
		glog.Errorf("Serving failed: %v", err)
	}
}
