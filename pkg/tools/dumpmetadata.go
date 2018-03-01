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
	"os"
	"strings"

	"github.com/davecgh/go-spew/spew"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	v1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"

	"github.com/Mirantis/virtlet/pkg/metadata"
)

const (
	// TODO: pass that as command line arg and use make default as constant
	// used there and in cmd/virtlet/virtlet.go
	virtletDBPath       = "/var/lib/virtlet/virtlet.db"
	defaultIndentString = " "
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
		fname, err := d.copyOutFile(&pod)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Can't extract metadata db for pod %q: %v\n", pod.Name, err)
			continue
		}
		defer os.Remove(fname)
		if err := dumpMetadata(fname); err != nil {
			fmt.Fprintf(os.Stderr, "Can't dump metadata for pod %q: %v\n", pod.Name, err)
		}
	}

	return nil
}

func (d *DumpMetadata) copyOutFile(pod *v1.Pod) (string, error) {
	fmt.Printf("Virtlet pod name: %s\n", pod.Name)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	stdin := bytes.NewBufferString("")

	exitCode, err := d.ExecInContainer(
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
		return "", err
	}

	if exitCode != 0 {
		return "", fmt.Errorf("remote command exit code was %d and its stderr output was:\n%s", exitCode, stderr.String())
	}

	f, err := ioutil.TempFile("/tmp", "virtlet-")
	defer f.Close()
	if err != nil {
		return "", fmt.Errorf("error during opening tempfile: %v\n", err)
	}
	f.Write(stdout.Bytes())

	return f.Name(), nil
}

func dumpMetadata(fname string) error {
	s, err := metadata.NewMetadataStore(fname)
	if err != nil {
		return fmt.Errorf("can't open metadata db: %v", err)
	}

	printlnIndented(1, "Sandboxes:")
	sandboxes, err := s.ListPodSandboxes(nil)
	if err != nil {
		return fmt.Errorf("can't list sandboxes: %v", err)
	}

	for _, smeta := range sandboxes {
		if sinfo, err := smeta.Retrieve(); err != nil {
			return fmt.Errorf("can't retrieve sandbox: %v", err)
		} else if err := dumpSandbox(smeta.GetID(), sinfo, s); err != nil {
			return fmt.Errorf("can't dump sandbox: %v", err)
		}
	}

	printlnIndented(1, "Images:")
	images, err := s.ImagesInUse()
	if err != nil {
		return fmt.Errorf("can't dump images: %v", err)
	}

	for image := range images {
		printlnIndented(2, image)
	}

	return nil
}

func dumpSandbox(podid string, sandbox *metadata.PodSandboxInfo, s metadata.MetadataStore) error {
	printlnIndented(2, "Sandbox id: %s", podid)
	printlnIndented(0, spew.Sdump(sandbox))

	printlnIndented(3, "Containers:")
	containers, err := s.ListPodContainers(podid)
	if err != nil {
		return fmt.Errorf("can't retrieve list of containers: %v", err)
	}

	for _, cmeta := range containers {
		printIndented(4, "Container id: %s", cmeta.GetID())
		if cinfo, err := cmeta.Retrieve(); err != nil {
			return fmt.Errorf("can't retrieve container metadata: %v", err)
		} else {
			printIndented(0, spew.Sdump(cinfo))
		}
	}

	return nil
}

func printlnIndented(level int, format string, data ...interface{}) {
	printIndented(level, format+"\n", data...)
}

func printIndented(level int, format string, data ...interface{}) {
	printIndentedWith(defaultIndentString, 2*level, format, data...)
}

func printIndentedWith(with string, length int, format string, data ...interface{}) {
	indentString := strings.Repeat(with, (length/len(with))+1)[:length]
	fmt.Printf("%s%s", indentString, fmt.Sprintf(format, data...))
}
