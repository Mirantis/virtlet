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

	"github.com/golang/glog"

	"github.com/Mirantis/virtlet/pkg/dhcp/server"
)

var (
	configDBPath = flag.String("db-path", "/run/virtlet-dhcp-config.db",
		"Path to configuration database")
	socketPath = flag.String("socket-path", "/run/virtlet-dhcp.sock",
		"Unix socket path to listen on waiting for configuration changes")
	dhcpIpAddress = flag.String("dhcp-ip", "0.0.0.0",
		"IP address to listen on for DHCP service")
)

func main() {
	flag.Parse()

	server, err := server.NewServer(*configDBPath)
	if err != nil {
		glog.Errorf("Initializing server failed: %v", err)
		os.Exit(1)
	}
	glog.V(1).Infof("Starting server on socket %s", *socketPath)
	if err = server.Serve(*socketPath, *dhcpIpAddress); err != nil {
		glog.Errorf("Serving failed: %v", err)
	}
}
