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
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
)

// The only aim of this outer qemu wrapper is to be able to do cleanup in case of
// libvirt terminates process with SIGKILL.
// It is done using pipe with read end passed to child inner wrapper, which
// in its turn watching for EOF on the pipe.

func main() {
	innerWrapper := "/inner_vmwrapper"
	cmd := exec.Command(innerWrapper, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	pipeR, _, _ := os.Pipe()

	cmd.ExtraFiles = []*os.File{
		pipeR,
	}

	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, syscall.SIGTERM)

	if err := cmd.Start(); err != nil {
		glog.Errorf("Failed to start %q: %v", innerWrapper, err)
		os.Exit(1)
	}

	go func() {
		for {
			<-sigChan
			glog.Infof("Outer wrapper: recieved SIGTERM. Sending to the child!")
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				fmt.Printf("Error on sending SIGTERM to child %v!", err)
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		glog.Errorf("Failed to run %q: %v", innerWrapper, err)
		os.Exit(1)
	}
}
