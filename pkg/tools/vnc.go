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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/renstrom/dedent"
	"github.com/spf13/cobra"
)

const (
	vncProtocolName  = "vnc"
	expectedHost     = "127.0.0.1"
	maxDisplayNumber = 0xffff - 5900
)

// vncCommand provides access to the VNC console of a VM pod
type vncCommand struct {
	client           KubeClient
	podName          string
	port             uint16
	output           io.Writer
	waitForInterrupt bool
}

// NewVNCCmd returns a cobra.Command that provides access to the VNC console of a VM pod.
func NewVNCCmd(client KubeClient, output io.Writer, waitForInterrupt bool) *cobra.Command {
	vnc := &vncCommand{client: client, output: output, waitForInterrupt: waitForInterrupt}
	cmd := &cobra.Command{
		Use:   "vnc pod [port]",
		Short: "Provide access to the VNC console of a VM pod",
		Long: dedent.Dedent(`
			This command forwards a local port to the VNC port used by the
			specified VM pod. If no local port number is provided, a random
			available port is picked instead. The port number is displayed
			after the forwarding is set up, after which the commands enters
			an endless loop until it's interrupted with Ctrl-C.
                `),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("pod name not specified")
			}
			vnc.podName = args[0]

			switch {
			case len(args) > 2:
				return errors.New("more than 2 options")
			case len(args) == 2:
				// port should be unprivileged and below the high ports
				// range
				var port int
				if port, err := strconv.Atoi(args[1]); err != nil || port < 1024 || port > 61000 {
					return errors.New("port parameter must be an integer number in range 1000-61000")
				}
				vnc.port = uint16(port)
			}
			return vnc.Run()
		},
	}
	return cmd
}

// Run executes the command.
func (v *vncCommand) Run() error {
	vmPodInfo, err := v.client.GetVMPodInfo(v.podName)
	if err != nil {
		return fmt.Errorf("can't get VM pod info for %q: %v", v.podName, err)
	}

	var buffer bytes.Buffer
	virshOutput := bufio.NewWriter(&buffer)
	exitCode, err := v.client.ExecInContainer(
		vmPodInfo.VirtletPodName, "libvirt", "kube-system",
		nil, virshOutput, os.Stderr,
		[]string{"virsh", "domdisplay", vmPodInfo.LibvirtDomainName()},
	)
	if err != nil {
		return fmt.Errorf("error executing virsh in Virtlet pod %q: %v", vmPodInfo.VirtletPodName, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("virsh returned non-zero exit code %d", exitCode)
	}

	virshOutput.Flush()
	parts := strings.Split(strings.Trim(buffer.String(), "\n"), ":")
	switch {
	case len(parts) != 3:
		return fmt.Errorf("virsh returned %q, while expected to return a value of a form %q", buffer.String(), "vnc://127.0.0.1:0")
	case parts[0] != vncProtocolName:
		return fmt.Errorf("virsh returned %q as a display protocol instead of expected %q", parts[0], vncProtocolName)
	case parts[1][:2] != "//":
		return fmt.Errorf("virsh returned %q after first ':' instead of expected %q", parts[1][:2], "//")
	case parts[1][2:] != expectedHost:
		return fmt.Errorf("virsh returned %q as a display host instead of expected %q", parts[1], expectedHost)
	}

	displayNumber, err := strconv.Atoi(parts[2])
	if err != nil || displayNumber < 0 || displayNumber > maxDisplayNumber {
		return fmt.Errorf("virsh returned %q as a display number which is expected to be in range 0..%d", parts[2], maxDisplayNumber)
	}

	pf := &ForwardedPort{
		RemotePort: 5900 + uint16(displayNumber),
		LocalPort:  v.port,
	}
	stopCh, err := v.client.ForwardPorts(vmPodInfo.VirtletPodName, "kube-system", []*ForwardedPort{pf})
	if err != nil {
		return fmt.Errorf("error forwarding the vnc port: %v", err)
	}
	defer close(stopCh)

	fmt.Fprintf(v.output, "VNC console for pod %q is available on local port %d\n", v.podName, pf.LocalPort)
	fmt.Fprintf(v.output, "Press ctrl-c or kill the process stop.\n")

	// if waitForInterrupt is set to false do not wait for interrupt (e.x. in tests).
	if v.waitForInterrupt {
		c := make(chan os.Signal, 2)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		<-c
	}

	return nil
}
