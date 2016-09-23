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
	etcdEndpoint = flag.String("etcd-endpoint", "http://0.0.0.0:2379",
		"etcd endpoint for client communication")
	listen = flag.String("listen", "/run/virtlet.sock",
		"The unix socket to listen on, e.g. /run/virtlet.sock")
)

func main() {
	flag.Parse()

	server, err := manager.NewVirtletManager(*libvirtUri, *pool, *storageBackend, *etcdEndpoint)
	if err != nil {
		glog.Errorf("Initializing server failed: %#v", err)
		os.Exit(1)
	}
	glog.Infof("Starting server on socket %s", *listen)
	if err = server.Serve(*listen); err != nil {
		glog.Errorf("Serving failed: %#v", err)
	}
}
