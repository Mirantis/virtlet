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
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Mirantis/virtlet/pkg/log"
	"github.com/golang/glog"
)

/*
* "virtlet_log" is an auxilary program that handles logs of VMs that are managed
* by Virtlet. It is designed to continously run in its own Docker container on Kubernetes,
* besides Virtlet container. It periodaically checks for new log folders and spawns
* one worker for each log folder. Worker then "tail -F"s log file and converts each line
* into JSON format that is expected by Kubernetes and lands output to the location where
* Kubernetes expects it. This way "kubectl logs <VM>" suddenly starts working, as well as
* the "Logs" tab on Kubernetes Dashboard.
 */

const (
	DEFAULT_SLEEP_SECONDS = 10
)

func main() {
	virtletDir := os.Getenv("VIRTLET_VM_LOGS")
	kubernetesDir := os.Getenv("KUBERNETES_POD_LOGS")
	if virtletDir == "" || kubernetesDir == "" {
		glog.V(1).Infoln("VIRTLET_VM_LOGS and KUBERNETES_POD_LOGS environment variables must be set")
		os.Exit(-1)
	}

	sleepInterval := DEFAULT_SLEEP_SECONDS
	if i, err := strconv.Atoi(os.Getenv("SLEEP_SECONDS")); err == nil {
		sleepInterval = i
	} else {
		glog.V(1).Infoln("Failed to parse SLEEP_SECONDS variable into int, fallback to 10 seconds.")
	}

	glog.V(1).Infof(`Starting VirtletLogger
VIRTLET_VM_LOGS:", %s
KUBERNETES_POD_LOGS:", %s)
SLEEP_SECONDS:", %d)
`, virtletDir, kubernetesDir, sleepInterval)

	logger := log.NewVirtletLogger(virtletDir, kubernetesDir)

	// Register callback for SIGINT and SIGTERM
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			if sig == syscall.SIGTERM || sig == syscall.SIGINT {
				logger.StopAllWorkers()
			}
		}
	}()

	// Infinite loop where we periodically check if there is any new log
	// file coming from libvirt. If a new one appears, we spawn a worker which
	// continuously reads the file and converts each line to Kubernetes-like
	// line on a location where Kubernetes expects it.
	for {
		fmt.Println("BUREK: SPAWN WORKERS")
		logger.SpawnWorkers()
		logger.StopObsoleteWorkers()

		glog.V(1).Infof("Sleep %d seconds\n", sleepInterval)
		time.Sleep(time.Duration(sleepInterval) * time.Second)
	}
}
