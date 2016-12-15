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
	"time"

	"github.com/Mirantis/virtlet/pkg/criproxy"

	"github.com/golang/glog"
)

const (
	// XXX: fix this
	connectionTimeout = 30 * time.Second
)

var (
	listen = flag.String("listen", "/run/criproxy.sock",
		"The unix socket to listen on, e.g. /run/virtlet.sock")
	connect = flag.String("connect", "/var/run/dockershim.sock",
		"CRI unix socket to connect to, e.g. /var/run/dockershim.sock")
)

func main() {
	flag.Parse()

	proxy, err := criproxy.NewRuntimeProxy(*connect, connectionTimeout)
	if err != nil {
		glog.Errorf("Initializing server failed: %v", err)
		os.Exit(1)
	}
	glog.V(1).Infof("Starting CRI proxy on socket %s", *listen)
	if err = proxy.Serve(*listen); err != nil {
		glog.Errorf("Serving failed: %v", err)
		os.Exit(1)
	}
}
