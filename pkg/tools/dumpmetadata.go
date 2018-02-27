/*
Copyright 2018 Mirantis

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

package tools

import (
	"bytes"
	"fmt"
	"io/ioutil"

	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

const (
	// TODO: pass that as command line arg and use make default as constant
	// used there and in cmd/virtlet/virtlet.go
	virtletDBPath = "/var/lib/virtlet/virtlet.db"
)

// DumpMetadata contains data needed by dump-metedata subcommand.
type DumpMetadata struct {
	SubCommandCommon
}

var _ SubCommand = &DumpMetadata{}

// RegisterFlags implements RegisterFlags method of SubCommand interface.
func (d DumpMetadata) RegisterFlags() {
}

// Run implements Run method of SubCommand interface.
func (d DumpMetadata) Run(clientset *typedv1.CoreV1Client, config *rest.Config, args []string) error {
	d.Setup(clientset, config)

	pods, err := d.GetVirtletPods()
	if err != nil {
		return err
	}

	if len(pods) == 0 {
		return fmt.Errorf("Not found any Virtlet pod")
	}

	for _, pod := range pods {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		stdin := bytes.NewBufferString("")

		fmt.Printf("pod: %s\n", pod.Name)
		exitCode, err := d.ExecCommandOnContainer(
			pod.Name,
			"virtlet",
			"kube-system",
			stdin,
			stdout,
			stderr,
			"/bin/cat",
			virtletDBPath,
		)
		if err != nil {
			fmt.Printf("  Error during downloading virtled metadata database: %v\n", err)
		}
		if exitCode != 0 {
			fmt.Printf(" Got different than expected exit code: %d\n", exitCode)
			fmt.Printf(" Remote command error output: %s\n", stderr.String())
			continue
		}
		f, err := ioutil.TempFile("/tmp", "virtlet-")
		defer f.Close()
		if err != nil {
			fmt.Printf(" Got error during opening tempfile: %v\n", err)
			continue
		}
		f.Write(stdout.Bytes())
		fmt.Printf("  Virtlet metadata database for this pod saved in %q location\n", f.Name())
	}

	return nil
}
