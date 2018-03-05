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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/renstrom/dedent"
	"github.com/spf13/cobra"

	"github.com/Mirantis/virtlet/pkg/metadata"
)

const (
	// TODO: pass that as command line arg and use make default as constant
	// used there and in cmd/virtlet/virtlet.go
	virtletDBPath       = "/var/lib/virtlet/virtlet.db"
	defaultIndentString = " "
)

// dumpMetadataCommand contains the data needed by the dump-metedata subcommand
// which is used to dump the contents of the Virtlet metadata db in a human-readable
// format.
type dumpMetadataCommand struct {
	client KubeClient
}

// NewDumpMetadataCmd returns a cobra.Command that dumps Virtlet metadata
func NewDumpMetadataCmd(client KubeClient) *cobra.Command {
	dump := &dumpMetadataCommand{client: client}
	cmd := &cobra.Command{
		Use:     "dump-metadata",
		Aliases: []string{"dump"},
		Short:   "Dump Virtlet metadata db",
		Long: dedent.Dedent(`
                        This command dumps the contents of Virtlet metadata db in
                        a human-readable format.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("This command does not accept arguments")
			}
			return dump.Run()
		},
	}
	return cmd
}

// Run executes the command.
func (d *dumpMetadataCommand) Run() error {
	podNames, err := d.client.GetVirtletPodNames()
	if err != nil {
		return err
	}

	if len(podNames) == 0 {
		return fmt.Errorf("No Virtlet pods found")
	}

	for _, podName := range podNames {
		fname, err := d.copyOutFile(podName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Can't extract metadata db for pod %q: %v\n", podName, err)
			continue
		}
		defer os.Remove(fname)
		if err := dumpMetadata(fname); err != nil {
			fmt.Fprintf(os.Stderr, "Can't dump metadata for pod %q: %v\n", podName, err)
		}
	}

	return nil
}

func (d *dumpMetadataCommand) copyOutFile(podName string) (string, error) {
	fmt.Printf("Virtlet pod name: %s\n", podName)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	stdin := bytes.NewBufferString("")

	exitCode, err := d.client.ExecInContainer(
		podName, "virtlet", "kube-system",
		stdin, stdout, stderr,
		[]string{"/bin/cat", virtletDBPath})
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
