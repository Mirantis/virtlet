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
	"strconv"
	"time"

	"github.com/Mirantis/virtlet/pkg/log"
)

func main() {
	virtletFolder := os.Getenv("VIRTLET_VM_LOGS")
	kubernetesFolder := os.Getenv("KUBERNETES_POD_LOGS")
	sleepInterval := 10 // seconds
	if i, err := strconv.Atoi(os.Getenv("SLEEP_SECONDS")); err == nil {
		sleepInterval = i
	}

	if virtletFolder == "" || kubernetesFolder == "" {
		fmt.Println("VIRTLET_VM_LOGS and KUBERNETES_POD_LOGS environment variables must be set")
		os.Exit(-1)
	}

	fmt.Println("starting VirtletLogger")
	fmt.Println("   VIRTLET_VM_LOGS:", virtletFolder)
	fmt.Println("   KUBERNETES_POD_LOGS:", kubernetesFolder)
	fmt.Println("   SLEEP_SECONDS:", sleepInterval)
	fmt.Println()

	logger := log.NewVirtletLogger(virtletFolder, kubernetesFolder)

	// Infinite loop where we periodically check if there is any new log
	// file coming from libvirt. If a new one appears, we spawn a worker for
	// it that continuously reads the file and converts each line to Kubernetes-like
	// line on a location where Kubernetes expects it.
	for {
		logger.SpawnWorkers()
		logger.StopObsoleteWorkers()

		fmt.Printf("sleep %d seconds\n", sleepInterval)
		time.Sleep(time.Duration(sleepInterval) * time.Second)
	}
}
